package jira

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Pool hands out clients for the instance a request asks for.
//
// One deployed jirrabit-mcp can serve many jirrabit instances, so the instance
// URL and API key travel with each tool call rather than living in the server's
// environment. A per-request http.Client would be correct but wasteful, so
// clients are cached and shared: connections get reused, and a short TTL lets a
// changed or rotated key take effect without a restart.
//
// The zero value is not usable; construct one with NewPool.
type Pool struct {
	defaultBaseURL string
	defaultAPIKey  string
	timeout        time.Duration
	maxRetries     int

	ttl        time.Duration
	maxEntries int
	validator  func(ctx context.Context, c *Client) error
	mu         sync.Mutex
	clients    map[string]*pooledClient
}

type pooledClient struct {
	client  *Client
	lastUse time.Time
}

// PoolOptions configures a Pool.
type PoolOptions struct {
	// DefaultBaseURL and DefaultAPIKey are the fallback used when a tool call
	// names no instance. They may both be empty, in which case every call must
	// supply its own.
	DefaultBaseURL string
	DefaultAPIKey  string
	Timeout        time.Duration
	MaxRetries     int
	// CacheTTL is how long an unused client is kept before being dropped. Zero
	// selects defaultCacheTTL.
	CacheTTL time.Duration
	// MaxEntries bounds the cache. Zero selects defaultMaxEntries.
	MaxEntries int
	// Validator confirms a (url, key) pair actually addresses a jirrabit before
	// the pair is cached. Optional; see Validate.
	Validator func(ctx context.Context, c *Client) error
}

const (
	defaultCacheTTL    = 10 * time.Minute
	defaultMaxEntries  = 256
	cacheSweepInterval = time.Minute
)

// NewPool returns a Pool. It does not require a default instance: a server
// whose every caller supplies its own target is a valid deployment.
func NewPool(opts PoolOptions) *Pool {
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	entries := opts.MaxEntries
	if entries <= 0 {
		entries = defaultMaxEntries
	}
	p := &Pool{
		defaultBaseURL: strings.TrimRight(strings.TrimSpace(opts.DefaultBaseURL), "/"),
		defaultAPIKey:  strings.TrimSpace(opts.DefaultAPIKey),
		timeout:        opts.Timeout,
		maxRetries:     opts.MaxRetries,
		clients:        make(map[string]*pooledClient),
	}
	if opts.Validator != nil {
		p.validator = opts.Validator
	}
	p.ttl = ttl
	p.maxEntries = entries
	return p
}

// Resolve returns the client for the requested instance, falling back to the
// pool's default when baseURL is empty.
//
// A caller-supplied apiKey with no baseURL is a configuration mistake rather
// than a request for the default instance, and is rejected: silently pairing a
// key with whatever instance the operator happened to configure would send one
// tenant's credentials to another tenant's data.
func (p *Pool) Resolve(ctx context.Context, baseURL, apiKey string) (*Client, error) {
	trimmedURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	trimmedKey := strings.TrimSpace(apiKey)
	// Whether the instance was named by the caller rather than defaulted, which
	// decides whether the SSRF guard applies.
	callerSupplied := trimmedURL != ""

	if trimmedURL == "" {
		if trimmedKey != "" {
			return nil, fmt.Errorf(
				"an apiKey was supplied without an instanceUrl; name the instance the key belongs to",
			)
		}
		if p.defaultBaseURL == "" {
			return nil, fmt.Errorf(
				"no instance given and this server has no default; pass instanceUrl and apiKey",
			)
		}
		trimmedURL, trimmedKey = p.defaultBaseURL, p.defaultAPIKey
	}
	if trimmedKey == "" {
		return nil, fmt.Errorf("an apiKey is required for %s", trimmedURL)
	}
	// Only a caller-supplied URL is checked. The operator's own default is a
	// deliberate choice and may legitimately be localhost or a private address,
	// which ValidateTarget refuses because a remote caller must not be able to
	// name those.
	if callerSupplied {
		if err := ValidateTarget(trimmedURL); err != nil {
			return nil, err
		}
	}

	key := cacheKey(trimmedURL, trimmedKey)
	p.mu.Lock()
	if existing, ok := p.clients[key]; ok {
		existing.lastUse = time.Now()
		p.mu.Unlock()
		return existing.client, nil
	}
	p.mu.Unlock()

	client, err := NewClient(Config{
		BaseURL:    trimmedURL,
		APIKey:     trimmedKey,
		Timeout:    p.timeout,
		MaxRetries: p.maxRetries,
	})
	if err != nil {
		return nil, err
	}

	// Prove the pair addresses a real jirrabit before caching it. A URL that is
	// reachable but is not jirrabit must fail here rather than on every
	// subsequent tool call with a confusing JSON parse error.
	if p.validator != nil {
		if err := p.validator(ctx, client); err != nil {
			return nil, err
		}
	}

	p.store(key, client)
	return client, nil
}

// ShaperFor returns a Shaper bound to the instance the client addresses, so
// `self` links point at the right deployment.
func (p *Pool) ShaperFor(client *Client) *Shaper {
	return NewShaper(client.BaseURL())
}

func (p *Pool) store(key string, client *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[key] = &pooledClient{client: client, lastUse: time.Now()}
	if len(p.clients) <= p.maxEntries {
		return
	}
	// Evict the least recently used entry.
	var oldestKey string
	var oldest time.Time
	for k, entry := range p.clients {
		if oldestKey == "" || entry.lastUse.Before(oldest) {
			oldestKey, oldest = k, entry.lastUse
		}
	}
	delete(p.clients, oldestKey)
}

// Sweep drops clients that have not been used within the TTL, closing their
// idle connections. Call it periodically from a long-running server.
func (p *Pool) Sweep() {
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := time.Now().Add(-p.ttl)
	for key, entry := range p.clients {
		if entry.lastUse.Before(cutoff) {
			entry.client.CloseIdleConnections()
			delete(p.clients, key)
		}
	}
}

// cacheKey identifies a (url, key) pair without holding the secret in a map
// key that might be logged. The URL is readable on purpose: it is not a secret,
// and a cache key that shows which instances are in use is useful when
// diagnosing.
func cacheKey(baseURL, apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return baseURL + "|" + hex.EncodeToString(sum[:8])
}

// ValidateTarget rejects instance URLs this server has no business calling.
//
// The concern is real: an MCP server that will fetch any URL a client names
// can be pointed at internal services and have the responses relayed back. Two
// cheap rules narrow that a great deal without getting in the way of the
// intended use — a self-hosted jirrabit on a private network address.
//
//   - Only http and https. No file:, gopher:, ftp: or anything else.
//   - No link-local, loopback-mapped or unspecified addresses, which is where
//     cloud instance-metadata endpoints live (169.254.169.254) and therefore
//     the most valuable pivot for an SSRF.
//
// A hostname is allowed through: it may legitimately resolve to a private
// address, and a shared server cannot know which. Operators who want a stricter
// policy set JIRRABIT_MCP_ALLOWED_HOSTS.
func ValidateTarget(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("instanceUrl is not a valid URL: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("instanceUrl must use http or https, got %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("instanceUrl is missing a host: %q", rawURL)
	}

	// Block the standard loopback names as well as loopback IP literals: a
	// caller naming "localhost" would otherwise walk straight past a check that
	// only inspects literal addresses. Full DNS resolution is deliberately not
	// done here — it would add a lookup and a failure mode to every new target
	// for a marginal gain, and ValidateJiraTarget is the real backstop anyway.
	if loopbackName(host) {
		return fmt.Errorf(
			"instanceUrl names the loopback host %q. A shared server must not let a caller "+
				"reach ports on the host it runs on; use the host's name or LAN address instead. "+
				"An operator who wants a local default should set JIRRABIT_URL", host)
	}

	if ip := net.ParseIP(host); ip != nil && blockedIP(ip) {
		return fmt.Errorf(
			"instanceUrl points at %s, which is a loopback, link-local or reserved address. "+
				"Cloud metadata endpoints live on those, and a shared server must not let a "+
				"caller reach ports on the host it runs on. Name the host or its LAN address "+
				"instead; an operator who wants a local default should set JIRRABIT_URL",
			ip,
		)
	}
	return nil
}

// loopbackName reports whether a hostname is one of the conventional loopback
// names. Anything ending in ".localhost" is reserved for loopback by RFC 6761.
func loopbackName(host string) bool {
	lowered := strings.ToLower(strings.TrimSuffix(host, "."))
	if lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return true
	}
	switch lowered {
	case "ip6-localhost", "ip6-loopback":
		return true
	}
	return false
}

// blockedIP reports whether an address is one this server refuses to let a
// caller reach.
//
// Loopback is included deliberately: a shared server that lets a remote caller
// address 127.0.0.1 is handing them the ability to probe whatever else runs on
// the MCP host. That does not constrain the normal case, because an operator
// pointing the server at their own jirrabit on localhost sets JIRRABIT_URL,
// and an operator-chosen default is not caller-supplied and is not checked.
func blockedIP(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() {
		return true
	}
	// 169.254.0.0/16 link-local, which covers the instance metadata service on
	// every major cloud. Also carrier NAT and the documentation ranges.
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, block := range []string{
		"100.64.0.0/10",
		"192.0.2.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
	} {
		if _, network, err := net.ParseCIDR(block); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
