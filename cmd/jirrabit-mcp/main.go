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
// agent's only map of this server, so it states the three things an agent cannot
// infer: the vocabulary is Jira's, the data is jirrabit's, and the instance is
// named per call.
const instructions = `This server exposes one or more jirrabit instances — a self-hosted
Jira-like tracker — using Atlassian's Jira tool names and payload shapes. Call
these tools exactly as you would call the official Atlassian Jira MCP server.

Choosing an instance. Every tool accepts instanceUrl and apiKey. Supply them to
work against a particular jirrabit; omit both to use this server's default
instance, which is what a single-user deployment configures. The apiKey comes
from that instance's own API keys page. An apiKey without an instanceUrl is
rejected rather than applied to the default, so one user's credentials are never
sent to another user's data.

What differs from real Jira:
  - cloudId is accepted and ignored; instanceUrl plays that role.
  - Descriptions and comments are stored as Markdown; they are returned as
    Atlassian Document Format and you send plain text.
  - Pagination is offset-based underneath, presented as an opaque nextPageToken.
    Pass that token back to continue; do not parse it.
  - Availability varies. Some Jira tools are not registered because jirrabit has
    no endpoint behind them yet, and a few report plainly that the operation is
    unsupported. Treat such a message as "not available here", not as a bug.

Start by calling getJiraCurrentUser. If you have no instance configured, it will
tell you so, and you can then supply instanceUrl and apiKey explicitly.`

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

	timeout := durationEnv("JIRRABIT_TIMEOUT", jira.DefaultTimeout)
	retries := intEnv("JIRRABIT_MAX_RETRIES", jira.DefaultMaxRetries)

	// The default instance is optional: a shared deployment supplies the
	// instance per call, and a personal one sets it here for convenience. The
	// pool only falls back to it when a call names no instance.
	defaultURL := strings.TrimSpace(os.Getenv("JIRRABIT_URL"))
	defaultKey := strings.TrimSpace(os.Getenv("JIRRABIT_API_KEY"))
	if defaultURL != "" && defaultKey == "" {
		return errors.New("JIRRABIT_URL is set but JIRRABIT_API_KEY is not; supply both, or neither to require callers to name their own instance")
	}

	pool := jira.NewPool(jira.PoolOptions{
		DefaultBaseURL: defaultURL,
		DefaultAPIKey:  defaultKey,
		Timeout:        timeout,
		MaxRetries:     retries,
		Validator:      jira.ValidateJiraTarget,
	})

	srv := server.NewMCPServer(
		"Jirrabit MCP Server",
		serverVersion,
		server.WithLogging(),
		server.WithToolCapabilities(true),
		server.WithRecovery(),
		server.WithInstructions(instructions),
	)

	tools.Deps{Pool: pool}.Register(srv, tools.Options{
		EnableDelete: tools.BoolEnv("JIRRABIT_MCP_ENABLE_DELETE"),
		EnableManage: tools.BoolEnv("JIRRABIT_MCP_ENABLE_MANAGE"),
	})

	if defaultURL != "" {
		log.Printf("[%s] default instance: %s", serverName, defaultURL)
	} else {
		log.Printf("[%s] no default instance; callers must pass instanceUrl and apiKey", serverName)
	}

	// Drop clients for instances nobody has touched in a while, so a long-lived
	// shared server does not hold sockets open to instances that are gone.
	stopSweeper := startSweeper(pool)
	defer stopSweeper()

	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "stdio":
		return serveStdio(srv)
	case "http", "streamable-http", "streamablehttp":
		return serveHTTP(srv, addr, path)
	default:
		return fmt.Errorf("unknown transport %q: use stdio or http", transport)
	}
}

// startSweeper runs pool.Sweep on a ticker for the life of the process. It only
// matters for the HTTP transport, where the process outlives any single session;
// on stdio the client owns the lifetime and an extra goroutine is noise.
func startSweeper(pool *jira.Pool) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pool.Sweep()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
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
