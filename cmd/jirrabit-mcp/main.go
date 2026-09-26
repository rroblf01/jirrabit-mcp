// Command jirrabit-mcp is an MCP server that exposes a jirrabit instance through
// the Atlassian Jira tool vocabulary.
//
// It is a pure MCP server: it registers tools and proxies them to jirrabit's
// REST API. It holds no AI provider credentials and makes no model calls — the
// MCP client owns the model.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/tools"
)

const serverName = "jirrabit-mcp"

const serverVersion = "1.0.0"

// instructions is what the server tells the client about itself. It is the
// agent's only map of this instance, so it states the two facts an agent cannot
// infer: the vocabulary is Jira's, and the data is jirrabit's.
const instructions = `This server exposes a jirrabit instance — a self-hosted Jira-like
tracker — using Atlassian's Jira tool names and payload shapes. Call these tools
exactly as you would call the official Atlassian Jira MCP server.

What differs from real Jira:
  - jirrabit is single-tenant, so cloudId is accepted and ignored.
  - Descriptions and comments are stored as Markdown; they are returned as
    Atlassian Document Format and you send plain text.
  - Pagination is offset-based underneath, presented as an opaque nextPageToken.
    Pass that token back to continue; do not parse it.
  - Availability varies. Some Jira tools are not registered because jirrabit
    has no endpoint behind them yet, and a few report plainly that the operation
    is unsupported. Treat such a message as "not available here", not as a bug.

Start with listJiraProjects to learn what exists, then getJiraIssue by key.`

func main() {
	transport := flag.String("transport", envOr("JIRRABIT_MCP_TRANSPORT", "stdio"),
		"MCP transport: stdio or http")
	addr := flag.String("addr", envOr("JIRRABIT_MCP_ADDR", ":8082"),
		"listen address for -transport http")
	path := flag.String("path", envOr("JIRRABIT_MCP_PATH", "/mcp"),
		"MCP endpoint path for -transport http")
	flag.Parse()

	if err := run(*transport, *addr, *path); err != nil {
		// Diagnostics go to stderr. On stdio, stdout carries the JSON-RPC
		// stream, so anything written there corrupts the protocol.
		fmt.Fprintf(os.Stderr, "jirrabit-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run(transport, addr, path string) error {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)

	client, err := jira.NewClient(jira.Config{
		BaseURL:    os.Getenv("JIRRABIT_URL"),
		APIKey:     os.Getenv("JIRRABIT_API_KEY"),
		Timeout:    durationEnv("JIRRABIT_TIMEOUT", jira.DefaultTimeout),
		MaxRetries: intEnv("JIRRABIT_MAX_RETRIES", jira.DefaultMaxRetries),
		UserAgent:  serverName + "/" + serverVersion,
	})
	if err != nil {
		return err
	}

	srv := server.NewMCPServer(
		"Jirrabit MCP Server",
		serverVersion,
		server.WithLogging(),
		server.WithToolCapabilities(true),
		server.WithRecovery(),
		server.WithInstructions(instructions),
	)

	deps := tools.Deps{Client: client, Shaper: jira.NewShaper(client.BaseURL())}
	deps.Register(srv, tools.Options{
		EnableDelete: tools.BoolEnv("JIRRABIT_MCP_ENABLE_DELETE"),
		EnableManage: tools.BoolEnv("JIRRABIT_MCP_ENABLE_MANAGE"),
	})

	log.Printf("[%s] configured against %s", serverName, client.BaseURL())

	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "stdio":
		return serveStdio(srv)
	case "http", "streamable-http", "streamablehttp":
		return serveHTTP(srv, addr, path)
	default:
		return fmt.Errorf("unknown transport %q: use stdio or http", transport)
	}
}

// serveStdio speaks MCP over stdin/stdout. The client owns the process, so a
// clean shutdown is just a return.
func serveStdio(srv *server.MCPServer) error {
	log.Printf("[%s] ready on stdio", serverName)
	// ServeStdio installs its own signal handling and returns on SIGINT/SIGTERM.
	return server.ServeStdio(srv)
}

// serveHTTP speaks MCP over streamable HTTP, which is what a client connecting
// to a server running as a service needs.
func serveHTTP(srv *server.MCPServer, addr, path string) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	httpSrv := server.NewStreamableHTTPServer(srv,
		server.WithEndpointPath(path),
		// Stateless: every request is self-contained, so the server can be run
		// behind a load balancer or scaled horizontally without sticky
		// sessions, and a dropped connection loses nothing.
		server.WithStateLess(true),
		// The anti-DNS-rebinding guard only accepts localhost by default, which
		// would reject every request on a real hostname. This server is meant to
		// sit behind the operator's own TLS terminator, so the guard is off and
		// the operator's proxy is the trust boundary.
		server.WithDisableLocalhostProtection(true),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() {
		log.Printf("[%s] ready on http://%s%s", serverName, addr, path)
		// Start reports http.ErrServerClosed after Shutdown; that is the
		// expected path, not a failure.
		if err := httpSrv.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Printf("[%s] shutting down", serverName)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

// --- env helpers -----------------------------------------------------------

// envOr reads an environment variable, falling back to a default.
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("[%s] %s=%q is not a number, using %d", serverName, name, raw, fallback)
		return fallback
	}
	return value
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		log.Printf("[%s] %s=%q is not a positive number of seconds, using %s", serverName, name, raw, fallback)
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
