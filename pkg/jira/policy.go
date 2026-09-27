package jira

import (
	"fmt"
	"net/url"
	"strings"
)

// HostPolicy is an operator's allowlist for the instances this server will call.
//
// It exists for the public-deployment case. A hosted jirrabit-mcp that anyone
// may point at any URL is, by construction, a proxy that will fetch whatever it
// is told to; the SSRF rules in pool.go close the obvious doors (loopback,
// link-local, no redirects) but a caller on the internet can still name a
// private address in the host's own network and read the answer back through a
// tool result. A self-hosted jirrabit on a LAN address is a legitimate target
// and must keep working, so the default stays permissive; an operator who does
// not want that sets JIRRABIT_MCP_ALLOWED_HOSTS and gets an exact list.
//
// An empty policy allows everything, which is the current behaviour.
type HostPolicy struct {
	// exact holds lowercased hosts, matched in full.
	exact map[string]bool
	// suffixes holds parent domains from entries written with a leading dot, so
	// ".empresa.com" matches "jirrabit.empresa.com" and "a.b.empresa.com".
	suffixes []string
}

// NewHostPolicy parses a comma-separated allowlist.
//
//	""                        -> no policy, everything allowed
//	"jirrabit.midominio.com"  -> that host only
//	".empresa.com"            -> that domain and any subdomain
func NewHostPolicy(raw string) *HostPolicy {
	policy := &HostPolicy{exact: map[string]bool{}}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, ".") {
			policy.suffixes = append(policy.suffixes, strings.TrimPrefix(entry, "."))
			continue
		}
		policy.exact[entry] = true
	}
	if len(policy.exact) == 0 && len(policy.suffixes) == 0 {
		return nil
	}
	return policy
}

// Permits reports whether a URL's host is on the list.
func (p *HostPolicy) Permits(rawURL string) bool {
	if p == nil {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	if p.exact[host] {
		return true
	}
	for _, suffix := range p.suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// Describe renders the policy for a start-up log, so an operator can see at a
// glance whether their allowlist took effect.
func (p *HostPolicy) Describe() string {
	if p == nil {
		return "none (any instance URL is accepted)"
	}
	parts := make([]string, 0, len(p.exact)+len(p.suffixes))
	for host := range p.exact {
		parts = append(parts, host)
	}
	for _, suffix := range p.suffixes {
		parts = append(parts, "*."+suffix)
	}
	return strings.Join(parts, ", ")
}

// CheckTarget validates a URL against the policy, returning an error a caller
// can act on: the allowlist is an operator decision, so a rejection should say
// so rather than looking like a broken target.
func (p *HostPolicy) CheckTarget(rawURL string) error {
	if p.Permits(rawURL) {
		return nil
	}
	host := rawURL
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	return fmt.Errorf(
		"this server only talks to its operator's allowlist of instances (%s), and %q is not on it. "+
			"If you run your own jirrabit, point your client at the instance its operator approved, "+
			"or run your own copy of this server, which will accept your URL",
		p.Describe(), host,
	)
}
