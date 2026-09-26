package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateTargetRejectsNonHTTP(t *testing.T) {
	for _, bad := range []string{
		"file:///etc/passwd",
		"gopher://localhost:8000/_",
		"ftp://example.com",
		"jirrabit.example.com", // no scheme
		"https://",             // no host
	} {
		if err := ValidateTarget(bad); err == nil {
			t.Errorf("ValidateTarget(%q) accepted it", bad)
		}
	}
}

// A shared server that will fetch any URL a client names is an SSRF risk. The
// most valuable pivot is the cloud instance-metadata endpoint, which lives on
// link-local addresses, so those must be refused outright.
func TestValidateTargetBlocksLinkLocalAndReserved(t *testing.T) {
	blocked := []string{
		"http://localhost:8000",                    // conventional loopback name
		"http://app.localhost:8000",                // RFC 6761 reserves the whole domain
		"http://LOCALHOST:8000",                    // case must not matter
		"http://127.0.0.1:8000",                    // loopback literal
		"http://[::1]:8000",                        // IPv6 loopback literal
		"http://169.254.169.254/latest/meta-data/", // AWS/GCP/Azure metadata
		"http://169.254.0.1/",
		"http://0.0.0.0:8000/",
		"http://100.64.0.1/", // carrier NAT
		"http://192.0.2.10/", // TEST-NET-1
		"http://198.18.0.1/", // benchmarking
		"http://[::1]:8000/", // IPv6 loopback as a literal
		"https://169.254.169.254/",
	}
	for _, bad := range blocked {
		if err := ValidateTarget(bad); err == nil {
			t.Errorf("ValidateTarget(%q) accepted a blocked address", bad)
		}
	}
}

// A self-hosted jirrabit on a private network is the intended deployment, and a
// hostname may legitimately resolve to a private address, so names and RFC1918
// addresses pass. Loopback does not, because a remote caller must not be able to
// probe the MCP host's own ports; an operator who wants that sets it as the
// default instead, which is not caller-supplied.
func TestValidateTargetAllowsHostnamesAndPrivateAddresses(t *testing.T) {
	allowed := []string{
		"http://jirrabit.example.com",
		"https://jirrabit.example.com",
		"http://10.0.0.5:8000",
		"http://192.168.1.20:8000",
		"http://[fd00::1]:8000",
	}
	for _, good := range allowed {
		if err := ValidateTarget(good); err != nil {
			t.Errorf("ValidateTarget(%q) rejected a legitimate target: %v", good, err)
		}
	}
}

// A key with no instance must never be paired with the operator's default: that
// would send one tenant's credential to another tenant's data.
func TestPoolRejectsKeyWithoutInstance(t *testing.T) {
	pool := NewPool(PoolOptions{DefaultBaseURL: "http://default.example", DefaultAPIKey: "default-key"})
	_, err := pool.Resolve(context.Background(), "", "some-other-key")
	if err == nil {
		t.Fatal("a key with no instanceUrl was accepted")
	}
	if !strings.Contains(err.Error(), "instanceUrl") {
		t.Errorf("error does not mention instanceUrl: %v", err)
	}
}

func TestPoolRequiresKey(t *testing.T) {
	pool := NewPool(PoolOptions{DefaultBaseURL: "http://default.example", DefaultAPIKey: "default-key"})
	if _, err := pool.Resolve(context.Background(), "http://other.example", ""); err == nil {
		t.Fatal("an instance with no key was accepted")
	}
}

func TestPoolRequiresAnInstanceSomewhere(t *testing.T) {
	pool := NewPool(PoolOptions{})
	_, err := pool.Resolve(context.Background(), "", "")
	if err == nil {
		t.Fatal("resolved with neither a default nor a per-call instance")
	}
	if !strings.Contains(err.Error(), "instanceUrl") {
		t.Errorf("error should point at instanceUrl: %v", err)
	}
}

// An operator-chosen default is trusted even when it is a loopback address,
// which is the common single-user setup. The guard exists for callers, not for
// the person running the server.
func TestPoolDefaultBypassesSSRFGuard(t *testing.T) {
	pool := NewPool(PoolOptions{DefaultBaseURL: "http://127.0.0.1:8000", DefaultAPIKey: "k"})
	if _, err := pool.Resolve(context.Background(), "", ""); err != nil {
		t.Errorf("a loopback default was rejected: %v", err)
	}
}

func TestPoolFallsBackToDefault(t *testing.T) {
	pool := NewPool(PoolOptions{DefaultBaseURL: "http://default.example/", DefaultAPIKey: "default-key"})
	client, err := pool.Resolve(context.Background(), "", "")
	if err != nil {
		t.Fatalf("default instance did not resolve: %v", err)
	}
	if client.BaseURL() != "http://default.example" {
		t.Errorf("base URL = %q, want the trailing slash trimmed", client.BaseURL())
	}
}

// A pair that does not answer jirrabit's own auth endpoint must be refused
// before it is cached, so a shared server cannot be used to reach arbitrary
// services and have their responses relayed.
func TestPoolValidatorRefusesNonJirrabitHost(t *testing.T) {
	// A server that answers 200 with something that is not a jirrabit payload.
	notJira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hello":"world"}`))
	}))
	defer notJira.Close()

	// httptest listens on loopback, which the guard refuses for caller-supplied
	// URLs. Passing it as the operator's default exercises the validator without
	// fighting the guard, and is also the realistic single-user shape.
	pool := NewPool(PoolOptions{DefaultBaseURL: notJira.URL, DefaultAPIKey: "any-key", Validator: ValidateJiraTarget})
	_, err := pool.Resolve(context.Background(), "", "")
	if err == nil {
		t.Fatal("a non-jirrabit host was accepted")
	}
	if !strings.Contains(err.Error(), "not a jirrabit instance") {
		t.Errorf("error should say the host is not a jirrabit instance: %v", err)
	}
}

func TestPoolValidatorReportsBadCredentials(t *testing.T) {
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"Invalid API key"}`))
	}))
	defer unauthorized.Close()

	pool := NewPool(PoolOptions{DefaultBaseURL: unauthorized.URL, DefaultAPIKey: "bad-key", Validator: ValidateJiraTarget})
	_, err := pool.Resolve(context.Background(), "", "")
	if err == nil {
		t.Fatal("a rejected key was accepted")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error should say the key was rejected: %v", err)
	}
}

func TestPoolCachesAndReusesClients(t *testing.T) {
	var hits int
	validator := func(ctx context.Context, c *Client) error {
		hits++
		return nil
	}
	pool := NewPool(PoolOptions{Validator: validator})

	first, err := pool.Resolve(context.Background(), "http://a.example", "key-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.Resolve(context.Background(), "http://a.example", "key-a")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("the same (url, key) pair did not reuse the cached client")
	}
	if hits != 1 {
		t.Errorf("validator ran %d times for one instance, want 1", hits)
	}

	// A different key for the same host must be a different client.
	other, err := pool.Resolve(context.Background(), "http://a.example", "key-b")
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Error("a different key reused the same client")
	}
}

// The cache key must not contain the secret itself, or it would be exposed by
// anything that logs the key.
func TestCacheKeyHidesTheSecret(t *testing.T) {
	key := cacheKey("http://a.example", "super-secret-token")
	if strings.Contains(key, "super-secret-token") {
		t.Errorf("cache key leaks the API key: %q", key)
	}
	if !strings.HasPrefix(key, "http://a.example|") {
		t.Errorf("cache key should stay readable about the host: %q", key)
	}
	if key == cacheKey("http://a.example", "other-token") {
		t.Error("two different keys produced the same cache key")
	}
}

func TestPoolEvictsBeyondMaxEntries(t *testing.T) {
	pool := NewPool(PoolOptions{MaxEntries: 4, Validator: func(context.Context, *Client) error { return nil }})
	for i := 0; i < 20; i++ {
		url := "http://host" + strings.Repeat("x", i+1) + ".example"
		if _, err := pool.Resolve(context.Background(), url, "key"); err != nil {
			t.Fatalf("resolving %s: %v", url, err)
		}
	}
	pool.mu.Lock()
	size := len(pool.clients)
	pool.mu.Unlock()
	if size > 4 {
		t.Errorf("cache holds %d entries, want at most 4", size)
	}
}
