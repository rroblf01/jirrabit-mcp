package tools

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// headerContext builds a context as the HTTP layer would leave it: the two
// credential headers stashed by HTTPContextFunc. Fabricated requests, no
// network — the point under test is precedence and validation, not transport.
func headerContext(url, key string) context.Context {
	r, _ := http.NewRequest("POST", "https://mcp.example.com/mcp", nil)
	if url != "" {
		r.Header.Set(HeaderInstanceURL, url)
	}
	if key != "" {
		r.Header.Set(HeaderAPIKey, key)
	}
	return HTTPContextFunc(context.Background(), r)
}

func TestHTTPContextFuncExtractsAndTrimsHeaders(t *testing.T) {
	r, _ := http.NewRequest("POST", "https://mcp.example.com/mcp", nil)
	r.Header.Set(HeaderInstanceURL, "  https://jirrabit.example.com/  ")
	r.Header.Set(HeaderAPIKey, "\tsecret\t")
	url, key := headerTarget(HTTPContextFunc(context.Background(), r))
	if url != "https://jirrabit.example.com/" || key != "secret" {
		t.Fatalf("headers not extracted and trimmed: %q %q", url, key)
	}
}

func TestHTTPContextFuncToleratesAbsence(t *testing.T) {
	url, key := headerTarget(HTTPContextFunc(context.Background(), nil))
	if url != "" || key != "" {
		t.Fatalf("nil request should yield no credentials: %q %q", url, key)
	}
	r, _ := http.NewRequest("POST", "https://mcp.example.com/mcp", nil)
	url, key = headerTarget(HTTPContextFunc(context.Background(), r))
	if url != "" || key != "" {
		t.Fatalf("headerless request should yield no credentials: %q %q", url, key)
	}
}

// resolveWith resolves through Deps.target with the given argument and header
// credentials, against a pool with no validator and no default, so nothing
// touches the network: resolution succeeds exactly when a complete,
// well-formed pair is assembled from the allowed sources.
func resolveWith(t *testing.T, args schema.Target, ctx context.Context) (*jira.Client, error) {
	t.Helper()
	d := Deps{Pool: jira.NewPool(jira.PoolOptions{})}
	client, _, err := d.target(ctx, args)
	return client, err
}

func TestExplicitArgumentsWinOverHeaders(t *testing.T) {
	client, err := resolveWith(t,
		schema.Target{InstanceURL: "https://a.example.com", APIKey: "key-a"},
		headerContext("https://b.example.com", "key-b"))
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL() != "https://a.example.com" {
		t.Fatalf("explicit arguments must win, got %s", client.BaseURL())
	}
}

func TestHeadersFillBothGaps(t *testing.T) {
	client, err := resolveWith(t,
		schema.Target{},
		headerContext("https://b.example.com", "key-b"))
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL() != "https://b.example.com" {
		t.Fatalf("header URL not used, got %s", client.BaseURL())
	}
}

func TestHeadersAndArgumentsMixPerField(t *testing.T) {
	client, err := resolveWith(t,
		schema.Target{InstanceURL: "https://a.example.com"},
		headerContext("", "key-b"))
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL() != "https://a.example.com" {
		t.Fatalf("argument URL with header key not assembled, got %s", client.BaseURL())
	}

	client, err = resolveWith(t,
		schema.Target{APIKey: "key-a"},
		headerContext("https://b.example.com", ""))
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL() != "https://b.example.com" {
		t.Fatalf("header URL with argument key not assembled, got %s", client.BaseURL())
	}
}

func TestNothingAnywhereIsStillAnError(t *testing.T) {
	_, err := resolveWith(t, schema.Target{}, context.Background())
	if err == nil || !strings.Contains(err.Error(), "instanceUrl") {
		t.Fatalf("expected a missing-credentials error, got %v", err)
	}
}

func TestHeaderKeyWithoutAURLIsRejected(t *testing.T) {
	// The no-mixed-tenants guard applies per source, not just to arguments: a
	// header key with no URL anywhere must not pair with anything by accident.
	_, err := resolveWith(t,
		schema.Target{},
		headerContext("", "orphan-key"))
	if err == nil || !strings.Contains(err.Error(), "without an instanceUrl") {
		t.Fatalf("expected a key-without-URL rejection, got %v", err)
	}
}

func TestHeaderURLGetsCallerSuppliedTreatment(t *testing.T) {
	// A header URL is caller-supplied exactly like an argument URL: loopback
	// must fail the SSRF check rather than slipping through on any exemption.
	_, err := resolveWith(t,
		schema.Target{},
		headerContext("http://127.0.0.1:9/", "key"))
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected an SSRF rejection for a header loopback URL, got %v", err)
	}
}
