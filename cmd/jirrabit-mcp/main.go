// Command jirrabit-mcp is an MCP server that exposes one or more jirrabit
// instances through Atlassian's Jira tool vocabulary.
//
// It is a pure MCP server. It holds no AI provider credentials and makes no
// model calls: it registers tools and proxies them to jirrabit's REST API. The
// MCP client owns the model.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
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

// serverVersion is a var, not a const, so the release build can stamp it with
// -ldflags "-X main.serverVersion=…". A const is inlined at compile time and the
// linker silently ignores -X for it, which looked like a working version stamp
// for as long as the Dockerfile passed one: every published image reported
// 1.0.0 and the ARG VERSION was decorative.
var serverVersion = "1.0.0"

// instructions is what the client is told about this server, and it is the only
// documentation an agent is guaranteed to read. Everything here is written to be
// sufficient on its own: an agent that has only these instructions and the tool
// schemas should be able to work without opening a README.
//
// It is deliberately concrete. Stating the actual JQL vocabulary, the actual
// error semantics and the actual list of things that do not work is what turns
// this from "call tools and guess" into "call tools and know".
const instructions = `You are working with jirrabit, a self-hosted issue tracker, through Atlassian's
Jira tool vocabulary. Call these tools exactly as you would call the official
Atlassian Jira MCP server; the names, arguments and payload shapes match.

CHOOSING AN INSTANCE
Every tool accepts instanceUrl and apiKey. Supply them to work against a
particular jirrabit; omit both to use this server's default instance. An apiKey
without an instanceUrl is rejected, so you cannot send one person's credentials
to another person's data. cloudId is accepted and ignored: jirrabit is
single-tenant per deployment, and instanceUrl plays that role.

STARTING A SESSION
Call getJiraCurrentUser to confirm the credentials work, then listJiraProjects to
see which projects you can reach. A project you cannot see does not appear at
all.

READING A 404
A 404 does not necessarily mean the key is wrong. jirrabit deliberately answers
404 for a project or issue the caller cannot see, so that key existence is not
leaked. Before concluding an issue is missing, consider that the API key's owner
may simply not be a member of its project.

JQL
searchJiraIssuesUsingJql is the tool to reach for first. The query language is a
subset of JQL:

  Fields: ` + tools.JQLFieldList + `
  Operators: ` + tools.JQLOperatorList + `
  An optional trailing "ORDER BY field [ASC|DESC]" — created, updated, priority, key.

Notes that save a round trip:
  - statusCategory takes the display names Jira uses, so
    statusCategory != Done and statusCategory = "In Progress" both work.
  - "is EMPTY" / "is not EMPTY" work on every field, including labels and
    assignee. "assignee is EMPTY" finds unassigned work.
  - assignee and reporter match on username, display name or full name.
  - A fragment with no operator at all is a free-text search over summary and
    description, so plain words work.
  - A clause that cannot be parsed is reported as an error, never as an empty
    result. If you get an error, the query is wrong; if you get zero results,
    the query worked and nothing matched.

  Example: project = WEB AND statusCategory != Done ORDER BY priority DESC

WRITING
Issue types, priorities and statuses are addressed by numeric id, and you cannot
invent one. Call listJiraIssueTypeMetadata, listJiraPriorities or
listJiraStatuses first, then pass the id:

  createJiraIssue  — takes issueTypeName (a name, resolved for you) and
                     assignee (a username, resolved for you).
  editJiraIssue    — for everything else, use the fields object:
                     {"priorityId": 4, "statusId": 2, "sprintId": 1}
                     An unrecognised key is an error, not a silent no-op.

Status changes are validated against the instance's workflow: a status that is
not reachable from the current one is rejected with an explanatory error. That
is a property of the instance, not a mistake on your part — read the error and
tell the user which transition the workflow forbids. Some instances have an
open workflow where any transition is allowed.

Time and dates:
  - dueDate is YYYY-MM-DD.
  - addOrEditJiraIssueWorklog takes either timeSpent ("2h 30m", "1d", "90m") or
    timeSpentSeconds. The result reports both timeSpent and timeSpentSeconds.

Comments, links and watchers:
  - addOrEditJiraIssueComment creates a comment. Passing commentId to edit one
    is not supported yet and says so.
  - createJiraIssueLink is directional: outwardIssueKey is the issue you are
    acting on, inwardIssueKey is the other end. Call listJiraIssueLinkTypes
    first rather than guessing a name.
  - watchJiraIssue defaults to watching; pass isWatching: false to unwatch.

READING RESULTS
  - Descriptions and comments are stored as Markdown and come back as Atlassian
    Document Format, which is a nested {type, version, content} structure. You
    send plain text and the server wraps it.
  - Lists come back as {"total", "startAt", "maxResults", "nextPageToken",
    "values"}. Pass nextPageToken back verbatim to continue. Do not parse it, do
    not construct it, and prefer it over startAt when paging.
  - An issue's status carries a statusCategory with key "new", "indeterminate" or
    "done". Agents usually want that rather than the status name.
  - Times in minutes on jirrabit's side are reported in seconds in the
    timeSpentSeconds and timespent fields.

NOT AVAILABLE HERE
These have no jirrabit endpoint yet, so the tools are not registered: JQL
aggregation functions, dashboards, boards, versions, components, entity
properties, attachments, remote links and changelogs. If a task needs one, say
so rather than looking for a tool that is not in the list.

The destructive tool deleteJiraIssue and the project-administration tool
updateJiraProject are only present when the operator has enabled them. If they
are absent, deletion is not something you can do through this server.`

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
	// A default instance on a server anyone can reach is not a convenience, it
	// is a leak: every caller whose own credentials are missing or rejected
	// silently spends the operator's key, on the operator's data, and the caller
	// walks away with the results. It therefore needs acknowledging — but only
	// where it is actually dangerous, because a client that spawns this binary
	// over stdio is single-user by construction, and a listener on loopback is
	// reachable only from this host. Neither needs a second environment
	// variable to do the obvious thing.
	if defaultURL != "" && publiclyReachable(transport, addr) && !truthy(os.Getenv("JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE")) {
		return errors.New(
			"JIRRABIT_URL is set and this server is listening on " + addr + ", so every caller " +
				"who supplies no instance would be served with this server's API key and could " +
				"read its data. A server bound to loopback, or one your client spawns over stdio, " +
				"needs no acknowledgement — this one does. Either set " +
				"JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE=1 to accept it deliberately, unset " +
				"JIRRABIT_URL and JIRRABIT_API_KEY so callers must name their own instance, or " +
				"bind to 127.0.0.1 and put a reverse proxy in front")
	}

	// The allowlist is the control that makes a published server safe. Its
	// absence is worth a log line rather than silence, because "everyone can use
	// it" and "anyone can make it fetch any URL on my network" are the same
	// server with this setting on or off.
	allowedHosts := jira.NewHostPolicy(os.Getenv("JIRRABIT_MCP_ALLOWED_HOSTS"))
	log.Printf("[%s] instance allowlist: %s", serverName, allowedHosts.Describe())

	pool := jira.NewPool(jira.PoolOptions{
		DefaultBaseURL: defaultURL,
		DefaultAPIKey:  defaultKey,
		Timeout:        timeout,
		MaxRetries:     retries,
		Validator:      jira.ValidateJiraTarget,
		AllowedHosts:   allowedHosts,
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
		log.Printf("[%s] ready on %s%s", serverName, displayAddr(addr), path)
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

// displayAddr renders a listen address for humans. ":8082" has no host part, and
// printing "http://:8082/mcp" reads like a broken URL rather than the wildcard
// bind it actually is.
func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://0.0.0.0" + addr
	}
	return "http://" + addr
}

// publiclyReachable reports whether this process is a server on a network
// interface other hosts can open a connection to.
//
// The distinction decides whether a default instance is a convenience or a
// disclosure. Over stdio the client started this process and speaks to it over
// pipes, so there is exactly one caller and it is the user. An HTTP listener on
// a loopback address is reachable only from this machine, which is what a
// private server behind a same-host reverse proxy looks like. Anything else —
// the wildcard ":8082", a specific public IP, a container's bridge address —
// means strangers can reach it.
func publiclyReachable(transport, addr string) bool {
	if transport != "http" && transport != "streamable-http" && transport != "streamablehttp" {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// An addr we cannot parse is one we should not assume is private.
		return true
	}
	if host == "" {
		// The wildcard bind: every interface, including the public one.
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname in the listen address. Loopback spelled as a name is the
		// only one that can be reasoned about here; anything else is a public
		// interface by assumption.
		return !strings.EqualFold(host, "localhost")
	}
	return !ip.IsLoopback()
}

// --- env helpers -----------------------------------------------------------

// truthy parses the spellings a human actually types for a boolean.
func truthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

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
