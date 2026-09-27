package jira

import (
	"strings"
	"testing"
)

func TestHostPolicyEmptyAllowsEverything(t *testing.T) {
	var policy *HostPolicy
	if NewHostPolicy("  ") != nil {
		t.Fatal("a blank allowlist should be no policy at all, not an empty one")
	}
	if !policy.Permits("https://anything.example.com") {
		t.Fatal("with no policy every host must be allowed, or self-hosted users are locked out")
	}
}

func TestHostPolicyExactHost(t *testing.T) {
	policy := NewHostPolicy("jirrabit.midominio.com")
	if !policy.Permits("https://jirrabit.midominio.com/") {
		t.Error("the listed host must be allowed")
	}
	if !policy.Permits("http://jirrabit.midominio.com:8000/") {
		t.Error("a port on the listed host must still be allowed")
	}
	for _, url := range []string{
		"https://otro.example.com/",
		"https://jirrabit.midominio.com.evil.example/", // suffix confusion
		"https://sub.jirrabit.midominio.com/",          // not the host itself
	} {
		if policy.Permits(url) {
			t.Errorf("%s must not be allowed by an exact-host policy", url)
		}
	}
}

func TestHostPolicySuffixMatchesSubdomains(t *testing.T) {
	policy := NewHostPolicy(".empresa.com")
	for _, url := range []string{
		"https://empresa.com/",
		"https://jirrabit.empresa.com/",
		"https://a.b.empresa.com/",
	} {
		if !policy.Permits(url) {
			t.Errorf("%s should match the .empresa.com suffix policy", url)
		}
	}
	for _, url := range []string{
		"https://empresa.com.evil.example/",
		"https://notempresa.com/",
	} {
		if policy.Permits(url) {
			t.Errorf("%s must not match .empresa.com", url)
		}
	}
}

func TestHostPolicyIsCaseInsensitive(t *testing.T) {
	policy := NewHostPolicy("Jirrabit.MiDominio.COM")
	if !policy.Permits("https://jirrabit.midominio.com/") {
		t.Error("hostnames are case-insensitive; the policy must be too")
	}
}

func TestHostPolicyMultipleEntries(t *testing.T) {
	policy := NewHostPolicy("a.example.com, b.example.com ,.c.example.com")
	for _, host := range []string{"a.example.com", "b.example.com", "x.c.example.com"} {
		if !policy.Permits("https://" + host) {
			t.Errorf("%s should be allowed", host)
		}
	}
	if policy.Permits("https://d.example.com/") {
		t.Error("d.example.com is not on the list")
	}
}

func TestHostPolicyCheckTargetExplainsItself(t *testing.T) {
	policy := NewHostPolicy("jirrabit.midominio.com")
	if err := policy.CheckTarget("https://jirrabit.midominio.com/"); err != nil {
		t.Fatalf("a permitted host must not error: %v", err)
	}
	err := policy.CheckTarget("https://otro.example.com/")
	if err == nil {
		t.Fatal("a host off the list must be rejected")
	}
	// The message has to name the host and the allowlist, or a user whose own
	// instance is refused has no idea what to do about it.
	for _, want := range []string{"otro.example.com", "jirrabit.midominio.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q; got: %v", want, err)
		}
	}
}

func TestHostPolicyRejectsGarbageURLs(t *testing.T) {
	policy := NewHostPolicy("jirrabit.midominio.com")
	if policy.Permits("http://") {
		t.Error("a URL with no host must not be permitted")
	}
	if policy.Permits("://nonsense") {
		t.Error("an unparseable URL must not be permitted")
	}
}

// The allowlist has to be enforced where a call is resolved, not merely parsed:
// that is the path every tool takes, and a policy that is only constructed and
// never consulted is the same as no policy at all.
func TestPoolEnforcesTheAllowlist(t *testing.T) {
	policy := NewHostPolicy("jirrabit.midominio.com")
	pool := NewPool(PoolOptions{AllowedHosts: policy})

	_, err := pool.Resolve(t.Context(), "https://otro.example.com/", "k")
	if err == nil {
		t.Fatal("an instance off the allowlist must be refused by the pool")
	}
	for _, want := range []string{"allowlist", "otro.example.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal should mention %q so a user knows it is policy, not a broken URL; got: %v", want, err)
		}
	}
}

func TestPoolWithoutAPolicyAcceptsAnyValidInstance(t *testing.T) {
	pool := NewPool(PoolOptions{})
	// A self-hosted jirrabit on a private address is the main use of this
	// server, so the default has to keep working.
	if _, err := pool.Resolve(t.Context(), "https://jirrabit.interno.example/", "k"); err != nil {
		t.Fatalf("with no allowlist a private instance must be accepted: %v", err)
	}
}

func TestPoolSkipsTheAllowlistForTheOperatorsOwnDefault(t *testing.T) {
	// An operator who set JIRRABIT_URL meant it, and their server may well be
	// reachable only on an address the allowlist would refuse.
	pool := NewPool(PoolOptions{
		DefaultBaseURL: "http://jirrabit-interno:8000/",
		DefaultAPIKey:  "k",
		AllowedHosts:   NewHostPolicy("jirrabit.midominio.com"),
	})
	if _, err := pool.Resolve(t.Context(), "", ""); err != nil {
		t.Fatalf("the operator's own default must not be filtered by the allowlist: %v", err)
	}
}
