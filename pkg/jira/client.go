package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout is the per-request timeout when none is configured.
const DefaultTimeout = 30 * time.Second

// DefaultMaxRetries is the number of retries for transient failures when none
// is configured.
const DefaultMaxRetries = 2

// apiPrefix is jirrabit's versioned REST root. The "v1" segment is deliberate:
// jirrabit reserves it for breaking schema changes without breaking clients.
const apiPrefix = "/api/v1/"

// Client talks to jirrabit's REST API using an API key.
//
// It is safe for concurrent use: the underlying *http.Client is shared and
// http.Client is itself safe for concurrent requests.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	maxRetries int
	// userAgent identifies the adapter in jirrabit's access logs.
	userAgent string
}

// Config configures a Client.
type Config struct {
	// BaseURL is the jirrabit instance root, e.g. "https://jirrabit.example.com".
	// A trailing slash is tolerated.
	BaseURL string
	// APIKey is the plaintext key from jirrabit's API keys page.
	APIKey string
	// Timeout bounds a single request. Zero means DefaultTimeout.
	Timeout time.Duration
	// MaxRetries bounds retries of transient failures. Zero means
	// DefaultMaxRetries; negative disables retrying entirely.
	MaxRetries int
	// UserAgent overrides the default User-Agent header.
	UserAgent string
}

// NewClient validates its configuration and returns a ready client.
//
// Both BaseURL and APIKey are mandatory. An empty or unparseable BaseURL is an
// error rather than a default, because a wrong base URL would silently point
// the agent at the wrong instance.
func NewClient(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("JIRRABIT_URL is required (e.g. http://localhost:8000)")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("JIRRABIT_URL is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("JIRRABIT_URL must use http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("JIRRABIT_URL is missing a host: %q", cfg.BaseURL)
	}

	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, errors.New("JIRRABIT_API_KEY is required (create one in jirrabit under your profile)")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	retries := cfg.MaxRetries
	if retries == 0 {
		retries = DefaultMaxRetries
	} else if retries < 0 {
		retries = 0
	}
	agent := cfg.UserAgent
	if agent == "" {
		agent = "jirrabit-mcp/1.0"
	}

	return &Client{
		baseURL: base,
		apiKey:  key,
		httpClient: &http.Client{
			Timeout: timeout,
			// Redirects would drop or leak the Authorization header to an
			// unvalidated host, so follow none.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxRetries: retries,
		userAgent:  agent,
	}, nil
}

// BaseURL returns the configured instance root, without a trailing slash.
func (c *Client) BaseURL() string { return c.baseURL }

// retryable reports whether a status code is worth retrying. Client errors are
// decisions, not hiccups: a 400, 401, 403, 404 or 429 must surface immediately,
// otherwise an agent waits out a delay for an answer that will never change.
func retryable(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// do performs one request with retries and returns the response body.
//
// out may be nil to discard the body. A non-2xx response is returned as an
// *APIError carrying jirrabit's own message.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	fullURL := c.baseURL + apiPrefix + strings.TrimLeft(path, "/")

	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		payload = encoded
	}

	// Transient failures get a short, bounded backoff: 150ms, 300ms, 600ms…
	// A jirrabit restart should not cost the agent its turn, but a genuinely
	// down instance should not be hammered either.
	backoff := 150 * time.Millisecond

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		resp, err := c.attempt(ctx, method, fullURL, payload)
		if err != nil {
			if attempt >= c.maxRetries {
				return fmt.Errorf("%s %s: %w", method, path, err)
			}
			if !sleepCtx(ctx, backoff) {
				return ctx.Err()
			}
			backoff *= 2
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Drain a bounded amount so the connection can be reused, then
			// decide whether this status is worth another attempt.
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()

			if retryable(resp.StatusCode) && attempt < c.maxRetries {
				if !sleepCtx(ctx, backoff) {
					return ctx.Err()
				}
				backoff *= 2
				continue
			}
			return &APIError{
				StatusCode: resp.StatusCode,
				Detail:     parseErrorBody(raw),
				Method:     method,
				Path:       "/" + strings.TrimLeft(path, "/"),
			}
		}

		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("reading %s %s response: %w", method, path, err)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decoding %s %s response: %w (body: %s)", method, path, err, truncate(raw, 300))
		}
		return nil
	}
}

func (c *Client) attempt(ctx context.Context, method, fullURL string, payload []byte) (*http.Response, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// sleepCtx waits for d, returning false if the context ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// --- verbs -----------------------------------------------------------------

// Get performs a GET and decodes the JSON body into out.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// Post performs a POST with a JSON body and decodes the reply into out.
func (c *Client) Post(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// Patch performs a PATCH with a JSON body and decodes the reply into out.
func (c *Client) Patch(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPatch, path, body, out)
}

// Put performs a PUT with a JSON body and decodes the reply into out.
func (c *Client) Put(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPut, path, body, out)
}

// Delete performs a DELETE, discarding the body unless out is non-nil.
func (c *Client) Delete(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodDelete, path, nil, out)
}

// --- page envelope ---------------------------------------------------------

// Page is jirrabit's list envelope. jirrabit paginates by offset (`page`,
// `size`) rather than by cursor, so this adapter re-shapes it into the opaque
// `nextPageToken` that Jira-trained agents expect. See cursor.go.
type Page struct {
	Count    int               `json:"count"`
	Page     int               `json:"page"`
	Size     int               `json:"size"`
	Pages    int               `json:"pages"`
	Next     *int              `json:"next"`
	Previous *int              `json:"previous"`
	Items    []json.RawMessage `json:"items"`
}

// List fetches a paged jirrabit endpoint and decodes each item into a T.
//
// It returns the page metadata alongside the decoded items so the caller can
// build a cursor from it.
func List[T any](ctx context.Context, c *Client, path string) ([]T, Page, error) {
	var raw Page
	if err := c.Get(ctx, path, &raw); err != nil {
		return nil, Page{}, err
	}
	items := make([]T, 0, len(raw.Items))
	for _, element := range raw.Items {
		var item T
		if err := json.Unmarshal(element, &item); err != nil {
			return nil, Page{}, fmt.Errorf("decoding item in %s: %w", path, err)
		}
		items = append(items, item)
	}
	return items, raw, nil
}
