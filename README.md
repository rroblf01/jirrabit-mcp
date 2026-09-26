# jirrabit-mcp

An MCP (Model Context Protocol) server that puts a [jirrabit](https://github.com/rroblf01/jirrabit)
instance behind Atlassian's Jira tool vocabulary.

It registers the tool names, parameter names and payload shapes an agent already
learned from the official Atlassian Jira MCP server — `getJiraIssue`,
`searchJiraIssuesUsingJql`, `createJiraIssue`, `transitionJiraIssue`,
`addOrEditJiraIssueComment` — so an agent that has used Jira needs no new
vocabulary.

The server holds **no AI provider credentials** and makes no model calls. It
registers tools and proxies them to jirrabit's REST API. The MCP client owns the
model.

## One server, many instances

This server is multi-tenant. Every tool takes `instanceUrl` and `apiKey`, so a
single deployed instance serves any number of jirrabit deployments — each caller
reaches their own with their own credentials, and this server stores none of
them.

```
"arguments": {
  "instanceUrl": "https://jirrabit.example.com",
  "apiKey": "…",
  "issueIdOrKey": "WEB-1"
}
```

`cloudId` is accepted and ignored, because Atlassian's tools require it and an
agent trained on them will send it.

You can also configure one default instance in the environment, in which case
callers omit the arguments. See [Configuration](#configuration).

## Quick start

### stdio — the client runs the binary

No port, no network listener, nothing to deploy. This is the usual local setup.

```bash
go build -o bin/jirrabit-mcp ./cmd/jirrabit-mcp/
```

```json
{
  "mcp": {
    "jirrabit": {
      "type": "local",
      "command": ["/absolute/path/to/bin/jirrabit-mcp"],
      "environment": {},
      "enabled": true
    }
  }
}
```

With no `JIRRABIT_URL` in the environment, every call must supply `instanceUrl`
and `apiKey`. To give callers a default so they can omit them:

```json
"environment": {
  "JIRRABIT_URL": "http://localhost:8000",
  "JIRRABIT_API_KEY": "…"
}
```

### Docker — run it as a service

```bash
docker compose up -d
```

Serves streamable HTTP on `:8082`, endpoint `/mcp`. The compose file adds
`host.docker.internal` so the container can reach a jirrabit running on the
Docker host.

For hacking on the server itself:

```bash
docker compose --profile dev run --rm jirrabit-mcp-dev
```

## Configuration

Everything is environment variables; there is no config file. See
[`.env.example`](.env.example) for the annotated list.

| Variable | Default | Meaning |
|---|---|---|
| `JIRRABIT_URL` | — | Optional default instance |
| `JIRRABIT_API_KEY` | — | Optional default instance's key |
| `JIRRABIT_TIMEOUT` | `30` | Per-request timeout, seconds |
| `JIRRABIT_MAX_RETRIES` | `2` | Retries for transport errors and 502/503/504 |
| `JIRRABIT_MCP_ENABLE_DELETE` | `false` | Register `deleteJiraIssue` |
| `JIRRABIT_MCP_ENABLE_MANAGE` | `false` | Register `updateJiraProject` |
| `JIRRABIT_MCP_TRANSPORT` | `stdio` | `stdio` or `http` |
| `JIRRABIT_MCP_ADDR` | `:8082` | Listen address for `http` |
| `JIRRABIT_MCP_PATH` | `/mcp` | Endpoint path for `http` |

`JIRRABIT_URL` and `JIRRABIT_API_KEY` must be set together or not at all. One
without the other is a startup error: a URL with no key can only mean "use a key
I was never given".

The destructive and project-administration tools are off by default, mirroring
the reference server's behaviour. Turning them on changes tool *visibility*;
jirrabit still decides who may actually delete something.

## Getting an API key

In jirrabit, open your profile → **API keys** → create one. The plaintext is
shown once: jirrabit stores only its SHA-256, so it cannot be recovered later.

That key carries your own permissions. An agent using it sees exactly the
projects you can see, and the API returns 404 rather than 403 for projects you
cannot, so project names are not leaked.

## What differs from real Jira

| | |
|---|---|
| `instanceUrl` | Replaces Atlassian's `cloudId` |
| `cloudId` | Accepted and ignored |
| Descriptions and comments | Stored as Markdown, returned as Atlassian Document Format. You send plain text |
| Pagination | Offset-based underneath, presented as an opaque `nextPageToken`. Pass it back; do not parse it |
| Availability | Not every Jira tool is registered, and a few report that the operation is unsupported. That means "not available here", not a bug |

## Available tools

Always on — 23 tools:

| Tool | jirrabit endpoint |
|---|---|
| `getJiraCurrentUser` | `GET /api/v1/me/` |
| `listJiraProjects` | `GET /api/v1/projects/` |
| `getJiraIssue` | `GET /api/v1/issues/{key}/` |
| `createJiraIssue` | `POST /api/v1/projects/{key}/issues/` |
| `editJiraIssue` | `PATCH /api/v1/issues/{key}/` |
| `transitionJiraIssue` | `PATCH /api/v1/issues/{key}/` with `status_id` |
| `searchJiraIssuesUsingJql` | `GET /api/v1/search?jql=` |
| `listJiraIssueComments` | `GET /api/v1/issues/{key}/comments/` |
| `addOrEditJiraIssueComment` | `POST /api/v1/issues/{key}/comments/` (create only) |
| `listJiraIssueWorklogs` | `GET /api/v1/issues/{key}/worklogs/` |
| `addOrEditJiraIssueWorklog` | `POST /api/v1/issues/{key}/worklogs/` (create only) |
| `listJiraIssueLinkTypes` | `GET /api/v1/link-types/` |
| `createJiraIssueLink` | `POST /api/v1/issues/{key}/links/` |
| `getJiraIssueLinks` | `GET /api/v1/issues/{key}/links/` |
| `listJiraSprints` | `GET /api/v1/projects/{key}/sprints/` |
| `getJiraSprint` | `GET /api/v1/sprints/{id}/` |
| `createJiraSprint` | `POST /api/v1/projects/{key}/sprints/` |
| `updateJiraSprint` | `PATCH /api/v1/sprints/{id}/` |
| `listJiraSavedFilters` | `GET /api/v1/filters/` |
| `getJiraUser` | `GET /api/v1/users/{id}/` or `GET /api/v1/users/search/?query=` |
| `watchJiraIssue` | `POST`/`DELETE /api/v1/issues/{key}/watchers/` |
| `listJiraStatuses` | `GET /api/v1/statuses/` |
| `listJiraPriorities` | `GET /api/v1/priorities/` |
| `listJiraIssueTypeMetadata` | `GET /api/v1/issue-types/` |

The three metadata tools exist so an agent can turn a status, priority or issue
type *name* into the numeric id that the write tools require, instead of
guessing.

Opt-in:

| Tool | Enabled by |
|---|---|
| `deleteJiraIssue` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraSprint` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `updateJiraProject` | `JIRRABIT_MCP_ENABLE_MANAGE` |

Still missing, because jirrabit exposes no endpoint for them: changelogs, boards,
versions, components, entity properties, attachments, and editing an existing
worklog. They will appear as the API grows; see the tool surface in
[AGENTS.md](AGENTS.md).

### One known gap

`LinkOut.created_by` is still a numeric user id while `source` and `target` are
keys. It is left as it is because changing it would break a second field for no
gain, but it is the one identifier in the payload that a caller cannot act on.
`getJiraUser` is the tool that turns it into a name.

## Verifying a deployment

```bash
go test ./...
go vet ./...

# The server's own prose has to match its own tool list. Needs no jirrabit, so
# it runs in CI on every push.
go run ./cmd/flowtest -phantoms-only -server ./bin/jirrabit-mcp

# End to end against a real jirrabit. Creates one issue, so use a scratch one.
JIRRABIT_URL=… JIRRABIT_API_KEY=… go run ./cmd/smoke -server ./bin/jirrabit-mcp

# Everything smoke does, plus 64 assertions about response shapes, the
# write/read round trip, the error paths, multi-tenant isolation and the opt-in
# gates. Add JIRRABIT_MCP_ENABLE_DELETE=1 JIRRABIT_MCP_ENABLE_MANAGE=1 to
# exercise the destructive tools too (73 checks).
JIRRABIT_URL=… JIRRABIT_API_KEY=… go run ./cmd/flowtest -server ./bin/jirrabit-mcp

# Two instances over one connection, with project isolation and the
# rejection paths (bad key, key without a URL, loopback URL).
go run ./cmd/multitenancy -server ./bin/jirrabit-mcp \
  -aURL … -aKey … -aProject … -bURL … -bKey … -bProject …

# Just list the tools a server registers.
JIRRABIT_URL=… JIRRABIT_API_KEY=… go run ./internal/probe ./bin/jirrabit-mcp
```

## Verifying a deployment

```bash
go test ./...
go vet ./...

# End to end against a real jirrabit. Creates one issue, so use a scratch one.
JIRRABIT_URL=… JIRRABIT_API_KEY=… go run ./cmd/smoke -server ./bin/jirrabit-mcp

# Two instances over one connection, with project isolation and the
# rejection paths (bad key, key without a URL, loopback URL).
go run ./cmd/multitenancy -server ./bin/jirrabit-mcp \
  -aURL … -aKey … -aProject … -bURL … -bKey … -bProject …

# Just list the tools a server registers.
JIRRABIT_URL=… JIRRABIT_API_KEY=… go run ./internal/probe ./bin/jirrabit-mcp
```

## Deploying on a VPS

1. `docker compose up -d` — or run the binary behind your own TLS terminator.
2. Put it behind a reverse proxy if it should not be reachable directly. The
   streamable HTTP endpoint is unauthenticated: authorisation happens per tool
   call through the caller's `apiKey`, but nothing stops an unauthenticated
   client from making calls. Anyone who reaches the endpoint can consume it.
3. If jirrabit is on the same host and reachable by name, set `JIRRABIT_URL` to
   it. If jirrabit is in a container, add `host.docker.internal` to jirrabit's
   `JIRRABIT_ALLOWED_HOSTS`, or Django answers 400.
4. Pin to a release rather than tracking `latest`.

## Requirements

Go 1.27 to build. One dependency: `github.com/mark3labs/mcp-go`. Runtime is a
single static binary, or the Alpine image.

## License

MIT. Not affiliated with, endorsed by, or sponsored by Atlassian.
