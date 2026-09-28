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

A default instance is a convenience for a server you run alone and a leak on one
you publish: any caller who supplies no `instanceUrl` — including every caller
whose own key is missing or rejected — is served with the operator's key and reads
the operator's data. So `run()` refuses to start when a default instance meets a
publicly-reachable listener, which is `-transport http` on anything but loopback.
Over stdio the client spawned the process, and a loopback bind is a private
server, so neither needs `JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE=1`; being precise
about *which* deployments are dangerous is what keeps the guard from being
friction every legitimate user has to work around. `publiclyReachable` assumes
the safe answer for an unparseable listen address and for a hostname in one.

`JIRRABIT_MCP_ALLOWED_HOSTS` is the control that makes a *published* server safe,
and it is checked per call, not once at start-up. Unset means any host that is
not loopback or link-local, which is right for a trusted LAN. Matching is exact
per entry, except that a leading dot matches a domain and its subdomains.

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
JIRRABIT_URL=… JIRRABIT_API_KEY=… \
  ./bin/jirrabit-mcp -transport http -addr 127.0.0.1:8082
```

The client then connects to `http://<host>:8082/mcp`. Binding to loopback and
terminating in a proxy is the shape to prefer: it is the only way to run a
default instance without acknowledging it, because `-addr :8082` listens on every
interface and the server will refuse to start alongside a default instance.

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
pkg/jira/policy.go         # operator allowlist for the instances this server will call
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
- **The allowlist is applied to caller-supplied URLs only, and never to the
  operator's default.** An operator who set `JIRRABIT_URL` meant it, and their
  own server may be reachable only at an address the allowlist would refuse. The
  bug that shaped this: `p.allowed` was first assigned inside
  `if opts.Validator != nil`, so the allowlist silently vanished for any pool
  built without a validator — an allowlist that can be switched off by accident
  is not an allowlist.
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
- **An argument a tool accepts and ignores is worse than one it refuses.**
  `strict.go` only catches arguments the schema does not *declare*, so a
  declared-then-unused argument passes the check and is dropped by the handler:
  four worklog arguments and two comment-visibility arguments did exactly that
  for a release, returning success for a write that never happened. When jirrabit
  cannot honour an Atlassian argument, either leave it out of the struct so
  `strict.go` refuses it, or refuse it in the handler with a message naming the
  API field that would have to exist. The second is better where the absence is
  worth explaining, which is why comment visibility and worklog estimates are
  handler guards and `fields`/`expand` are simply not declared.
- **A unit test that builds a DTO by hand cannot see a wrong JSON tag.** The
  shaper tests construct `jira.Issue{TypeID: 4}` and check it comes out the
  other end, which passes whatever the tag says. `TestIssueDTOUnmarshalsJirrabitsFieldNames`
  unmarshals a body shaped like jirrabit's `IssueOut` instead, and exists because
  a tag guessed the response shape's field name rather than the API's and every
  other test stayed green. The same applies to a schema the handler does not
  bind: `listJiraSprints` advertised `SprintArgs` and read `ListSprintsArgs`, so
  its own paging arguments were rejected as unknown. The registered schema and
  the bound struct are two declarations of the same thing, and nothing but a test
  compares them.
- **Diff the endpoints against the tools, do not eyeball the tool list.** The
  surface grew in releases, and a release adds what someone needed at the time.
  Counting `NewTool(` against the `@api.` decorators found 118 endpoint methods
  served by 86 tools, and 34 of the gaps were invisible from the tool side:
  `createJiraProject` existed with no way to add a second person to the project it
  created, `createJiraStatus` existed with no way to rename what it created, and
  CSV import, saved board views, recently-viewed and mentions had neither an
  endpoint nor a tool. The command is
  `rg -o '@api\.(get|post|patch|put|delete)\("' jirrabit/api.py | wc -l`
  against `rg -o 'NewTool\("' pkg/tools/*.go | wc -l`, and the two numbers are
  only a rough guide — several tools share an endpoint and some endpoints are
  internal previews.
- **A unique column needs a pre-check, or a duplicate is a 500 with a traceback.**
  `Team.slug` is unique; neither the create nor the rename looked, so both reached
  the insert and came back as an `IntegrityError`. The web UI's form never showed
  it, which is exactly why it survived — the API was the only way to reach it. The
  same class of bug as the missing `max_length`, and the same fix: check before
  the write and answer 409. The check has to allow a row to keep its own value, or
  a description-only edit becomes impossible.
- **An embedded struct is the bare type name.** `Confirmation Confirmation` is a
  *named field*, not an embed, and the generated schema then exposes a property
  called `Confirmation` — so `confirm` arrives as an unknown argument and
  `strict.go` refuses the confirmation call of every two-step delete. The symptom
  is a delete that previews and then cannot be confirmed, which reads as a
  confirmation bug rather than a schema one. A gofmt pass will not fix it, and
  neither will a `json:",inline"` tag, which this mcp-go version ignores.
  `cmd/flowtest` catches it, because the second call is the check.
- **Board placement is a set of endpoints, not a field.** `rank` is a card's
  index inside its `(project, status)` column, and a column is a dense `0..n-1`
  run. That makes it an invariant rather than a value, so there is deliberately
  no endpoint that sets a rank: `moveJiraIssue` and `reorderJiraBoardColumn` go
  through the same helpers the drag uses, which renumber the whole column. The
  consequences are worth knowing before changing either. A *short* key list is
  legal and means "these to the top, the rest keep their order", because a stale
  view of the board sends one and refusing it would make the board unusable the
  moment somebody else moved a card. And `bulkUpdateJiraBoard` refuses `delete`
  with a pointer at `deleteJiraIssue` — the only action there that cannot be
  undone should not be reachable from a call that takes a list, or the two-step
  confirmation would be a speed bump with a way around it.
- **A relation read from an async view is a query, and the cache is not
  guaranteed.** `aget_or_create` returns the row it just created *with* the
  objects it was handed cached, and an existing row with neither cached. A
  response builder that reads `pin.issue.key` therefore works on the first call
  and 500s on the second, which is a bug that only shows up on the retry. Three
  of jirrabit's new endpoints had it; the fix is to `select_related` the paths a
  builder reads, and to re-fetch after `aget_or_create` rather than trust what it
  returned. Related: a schema field left with no default is a shape that cannot
  be built, and `ty` is the only thing that notices.
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
- The image is `FROM scratch` and the Dockerfile is the multi-arch contract.
  Two things in it are load-bearing, and both were found the hard way:
  `ARG TARGETOS`/`ARG TARGETARCH` are declared **without a default**, because a
  declared default beats the value BuildKit injects — with `=amd64` the
  `linux/arm64` build produced an x86-64 binary inside an image labelled arm64,
  which pulls fine and then fails with "exec format error" on every ARM host.
  And `HEALTHCHECK` is exec-form, because scratch has no `/bin/sh`: a
  shell-form check cannot run at all, and the container reports healthy anyway.
  `main.serverVersion` has to stay a `var`, too: `-X` is silently ignored for a
  `const`, so every image reported `1.0.0` no matter what `VERSION` said.
  The image is published by `.github/workflows/publish.yml` on release, to
  `ghcr.io/rroblf01/jirrabit-mcp`.

## Verification

```bash
go test ./...                                   # pure functions and the pool/SSRF guard
go vet ./...
gofmt -l .                                       # must print nothing

# No jirrabit needed. Scans the instructions and every tool description for
# tool-shaped names that are not registered, and prints the tool count.
go run ./cmd/flowtest -server ./bin/jirrabit-mcp -phantoms-only

# Everything. 266 checks, and it creates real data, so point it at a scratch
# instance. Add JIRRABIT_MCP_ENABLE_DELETE=1 and _ENABLE_MANAGE=1 to exercise the
# opt-in tools too; several checks assert the opposite answer when the flags are
# off, so running it both ways is the point.
JIRRABIT_URL=… JIRRABIT_API_KEY=… \
  go run ./cmd/flowtest -server ./bin/jirrabit-mcp

JIRRABIT_URL=… JIRRABIT_API_KEY=… \
  go run ./cmd/smoke -server ./bin/jirrabit-mcp  # end-to-end against a real jirrabit

go run ./cmd/multitenancy -server ./bin/jirrabit-mcp \
  -aURL … -aKey … -aProject … -bURL … -bKey … -bProject …
```

`flowtest` twice in a row against a local Postgres can exhaust its connections
(`max_connections=100`) and answer `sorry, too many clients already`. It is
transient — wait a few seconds between runs. A check that fails once with that
message and passes on a re-run is the database, not the code.

`smoke` and `multitenancy` create data, so point them at a scratch instance.
`multitenancy` deliberately runs with no default instance configured, which is
the shared-server case.

Unit tests cover the pure functions — ADF conversion, cursor encoding, duration
parsing, shape conversion — plus the pool's validation and caching. Those are
where silent corruption hides; the two commands cover the rest.
