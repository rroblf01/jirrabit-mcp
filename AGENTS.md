# jirrabit-mcp

MCP (Model Context Protocol) server that exposes a [jirrabit](https://github.com/rroblf01/jirrabit)
instance through the Atlassian Jira tool vocabulary — the same tool names,
parameter names and payload shapes an agent already learned from the official
Atlassian MCP server.

This process is a **pure MCP server**. It holds no AI provider credentials and
makes no model calls; it registers tools and proxies them to jirrabit's REST
API. The MCP client (Claude Code, OpenCode, …) owns the model.

## Build & Run

```bash
go build -o bin/jirrabit-mcp ./cmd/jirrabit-mcp/

./bin/jirrabit-mcp                  # stdio (default)
./bin/jirrabit-mcp -transport http  # streamable HTTP on :8082
```

No Makefile. `go build` / `go vet` / `go test` are the whole toolchain.

## Configuration

All environment variables, no config file. See `.env.example` for the annotated
list. The two required ones:

| Variable | Meaning |
|---|---|
| `JIRRABIT_URL` | Base URL of the jirrabit instance, no trailing slash |
| `JIRRABIT_API_KEY` | API key from jirrabit's API keys page, sent as a bearer token |

A missing required variable is a **startup error**. There is deliberately no
default URL and no "try localhost and carry on" behaviour — a silent fallback
would point the agent at the wrong instance, or at nothing.

## Connecting a client

stdio — the client spawns the binary, no port and no network listener:

```json
{
  "mcp": {
    "jirrabit": {
      "type": "local",
      "command": ["/absolute/path/to/bin/jirrabit-mcp"],
      "environment": {
        "JIRRABIT_URL": "http://localhost:8000",
        "JIRRABIT_API_KEY": "…"
      },
      "enabled": true
    }
  }
}
```

streamable HTTP — for running the server as a service:

```bash
JIRRABIT_URL=… JIRRABIT_API_KEY=… ./bin/jirrabit-mcp -transport http
```

The client then connects to `http://<host>:8082/mcp`.

## Architecture

```
cmd/jirrabit-mcp/main.go   # entrypoint: env parsing, transport selection, tool registration
pkg/jira/client.go         # HTTP client for jirrabit's /api/v1/, bearer auth, retries
pkg/jira/errors.go         # maps jirrabit's {"detail": …} envelope onto MCP tool errors
pkg/jira/shapes.go         # jirrabit DTOs -> Jira-shaped {id, key, self, fields:{…}}
pkg/jira/adf.go            # plain text <-> Atlassian Document Format
pkg/jira/cursor.go         # jirrabit's page/size <-> the opaque nextPageToken agents expect
pkg/schema/types.go        # typed argument structs for tools with complex input schemas
pkg/tools/*.go             # one file per Atlassian tool group (issues, comments, …)
```

Request flow: a tool handler reads its arguments → `pkg/jira` client builds the
`/api/v1/` request → jirrabit's django-ninja returns its own DTO → `shapes.go`
re-shapes it into the Jira payload the agent expects → result is returned as
MCP text content.

## Design decisions that are not obvious from the code

- **jirrabit stays a REST API, not a Jira clone.** Nothing Jira-shaped is
  persisted in jirrabit. The Jira vocabulary lives entirely in this repo, so
  the two can be versioned and deployed independently.
- **`cloudId` is accepted and ignored.** Atlassian's tools require it on every
  call. Requiring it here would break every agent trained against Jira, so each
  tool takes it as an optional parameter and no-ops on it.
- **Markdown is converted to ADF here, not in jirrabit.** jirrabit stores
  Markdown; Atlassian Document Format is a Jira-wire concern, so it belongs in
  the adapter. Tools therefore take plain text and this repo does the wrapping.
- **`nextPageToken` is synthesized.** jirrabit paginates with `page`/`size`;
  agents expect a cursor. `cursor.go` encodes the position in an opaque token so
  the cursor-shaped contract holds even though the backend is offset-based.
- **Delete and project-management tools are opt-in**, gated by
  `JIRRABIT_MCP_ENABLE_DELETE` / `JIRRABIT_MCP_ENABLE_MANAGE`, mirroring the
  reference server's behaviour. Visibility only — jirrabit still authorises.

## Conventions

- Go 1.27, single dependency: `github.com/mark3labs/mcp-go v1.0.0`. Prefer the
  standard library over adding a second module; there is no HTTP client
  dependency because `net/http` is sufficient.
- Read tool arguments with `request.GetString("key", "default")` for simple
  tools, and `request.BindArguments(&args)` with a `pkg/schema` struct when the
  input is nested or typed.
- Log messages, tool descriptions, server instructions and documentation are
  all in English. Commit messages are in English.
- Return `mcp.NewToolResultError(...)` for expected failures (not found, no
  permission, invalid JQL). Return a Go `error` only for genuine bugs.
- Announce safety with `mcp.WithReadOnlyHintAnnotation`, `WithDestructiveHintAnnotation`,
  `WithIdempotentHintAnnotation` and `WithOpenWorldHintAnnotation`. Agents read
  these before calling.
- The `docs/` and `AGENTS.md` files are tracked. Do **not** add a `*.md` line to
  `.gitignore`.

## Testing

`go test ./...` covers the pure functions — ADF conversion, cursor encoding and
shape conversion — which is where silent corruption hides. Tool handlers are
exercised end to end by connecting a real MCP client to a real jirrabit.
