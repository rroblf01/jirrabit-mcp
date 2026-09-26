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
list.

**This server is multi-tenant by default.** One deployed jirrabit-mcp serves any
number of jirrabit instances: every tool takes `instanceUrl` and `apiKey`, and
the caller supplies the credentials for their own instance. The server holds no
credentials of its own.

| Variable | Meaning |
|---|---|
| `JIRRABIT_URL` | Optional *default* instance base URL |
| `JIRRABIT_API_KEY` | Optional *default* instance API key |

`JIRRABIT_URL` and `JIRRABIT_API_KEY` must be set **together or not at all** —
one without the other is a startup error, because a URL with no key can only mean
"use a key I was never given". Setting both is the convenience case for a
personal single-instance server; leaving both unset is the shared case.

A key supplied without an instance is rejected rather than applied to the
default, so one user's credentials can never be sent to another user's data.

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
cmd/smoke/                 # end-to-end check: drives the server as a real MCP client
cmd/flowtest/              # every tool's response shape, the error paths, and a
                           # check that the prose names no unregistered tool
cmd/isolationtest/         # cross-tenant isolation: needs two live instances, so
                           # it is deliberately not in CI
pkg/tools/sprints.go       # sprint read/create/update, delete behind the flag
pkg/tools/filters.go       # saved filters and single-user lookup
cmd/multitenancy/          # proves two instances can be served over one connection
internal/probe/            # lists a server's registered tools over stdio
pkg/jira/client.go         # HTTP client for jirrabit's /api/v1/, bearer auth, retries
pkg/jira/pool.go           # per-call instance resolution, client cache, SSRF guard
pkg/jira/validate.go       # confirms a (url, key) pair really addresses a jirrabit
pkg/jira/errors.go         # maps jirrabit's {"detail": …} envelope onto MCP tool errors
pkg/jira/dto.go            # jirrabit's wire types
pkg/jira/shapes.go         # jirrabit DTOs -> Jira-shaped {id, key, self, fields:{…}}
pkg/jira/adf.go            # plain text <-> Atlassian Document Format
pkg/jira/cursor.go         # jirrabit's page/size <-> the opaque nextPageToken agents expect
pkg/jira/duration.go       # Jira's "2h 30m" -> minutes
pkg/schema/types.go        # typed argument structs; every one embeds schema.Target
pkg/tools/*.go             # one file per Atlassian tool group (issues, comments, …)
```

Request flow: a tool handler binds its arguments → `Deps.target` resolves
`instanceUrl`/`apiKey` through the pool to a client for that instance, validating
it on first sight → the client builds the `/api/v1/` request → jirrabit's
django-ninja returns its own DTO → `shapes.go` re-shapes it into the Jira payload
the agent expects → returned as MCP text content. The `Shaper` is built per call
from the resolved client, so `self` links point at the instance the data came
from.

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
- **Clients are cached, keys are not logged.** The pool keys entries on
  `url|sha256(key)`, never the secret itself, and the cache key is swept on a TTL
  so a rotated or forgotten key stops being used.
- **Delete and project-management tools are opt-in**, gated by
  `JIRRABIT_MCP_ENABLE_DELETE` / `JIRRABIT_MCP_ENABLE_MANAGE`, mirroring the
  reference server's behaviour. Visibility only — jirrabit still authorises.
- **Two tools that look like gaps are not.** `getJiraUser` is not a second
  `getJiraCurrentUser`: that answers "who am I", this answers "who is this
  person", which is the question behind resolving an assignee id. And
  `updateJiraSprint` does not close a sprint, because jirrabit's close carries
  unfinished issues to another sprint and no endpoint does only that; its
  description says so rather than letting an agent assume.
- **Discover the project key, never hardcode it.** flowtest and cmd/smoke read it
  from `listJiraProjects` (override with `-project`). flowtest originally
  asserted a literal "DEMO" and passed on the instance it was written against
  while failing 34 checks on any other — a portability test that is not portable
  is worse than none, because it reads as coverage.
- **The prose is part of the interface.** A tool named in the server
  instructions or in another tool's description that is not registered makes an
  agent call `transitionJiraIssue`, get "tool not found", and stop. It already
  happened: `editJiraIssue`'s description and the README both pointed at
  `transitionJiraIssue` before the tool existed. `cmd/flowtest -phantoms-only`
  scans the instructions and every description for tool-shaped names and fails
  if one is unregistered; it runs in CI and needs no jirrabit. Run it after
  touching any description, and exempt deliberately opt-in tools in
  `optInTools`.

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

## Verification

```bash
go test ./...                                   # pure functions and the pool/SSRF guard
go vet ./...

JIRRABIT_URL=… JIRRABIT_API_KEY=… \
  go run ./cmd/smoke -server ./bin/jirrabit-mcp  # end-to-end against a real jirrabit

go run ./cmd/multitenancy -server ./bin/jirrabit-mcp \
  -aURL … -aKey … -aProject … -bURL … -bKey … -bProject …
```

`smoke` and `multitenancy` create data, so point them at a scratch instance.
`multitenancy` deliberately runs with no default instance configured, which is
the shared-server case.

Unit tests cover the pure functions — ADF conversion, cursor encoding, duration
parsing, shape conversion — plus the pool's validation and caching. Those are
where silent corruption hides; the two commands cover the rest.
