# jirrabit-mcp

An MCP (Model Context Protocol) server that puts a [jirrabit](https://github.com/rroblf01/jirrabit)
instance behind Atlassian's Jira tool vocabulary.

**Current release: 1.3.0** ([changelog](CHANGELOG.md)) — 114 tools registered
without any flag, 126 with both on.

1.2.0 closed the gap between jirrabit's REST API and this server: 34 tools that
were the last thing missing, including the six whose endpoints did not exist at
all and had to be written — CSV import and export, saved board views,
recently-viewed issues, mention receipts and registration invites. If you script
`deleteJiraSprint` or `deleteJiraEpic`, they now take two calls; see
[Upgrading](CHANGELOG.md#upgrading).

1.2.1 is a patch: two fixes to the repo's own checks and docs, with no change to
what an agent calls.

## Try it in one minute

A public instance is already running, against a public jirrabit you can also look
at in a browser. No account, no install, no API key of your own.

### OpenCode

In your `opencode.json` (globally at `~/.config/opencode/opencode.json`, or
per-project):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "jirrabit": {
      "type": "remote",
      "url": "https://jirrabit-mcp.ricardorobles.es/mcp",
      "enabled": true
    }
  }
}
```

To stop passing credentials on every call, send them once as headers instead.
`{env:VAR}` keeps the secret out of the file:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "jirrabit": {
      "type": "remote",
      "url": "https://jirrabit-mcp.ricardorobles.es/mcp",
      "enabled": true,
      "headers": {
        "X-Jirrabit-Instance-Url": "https://jirrabit.ricardorobles.es",
        "X-Jirrabit-Api-Key": "{env:JIRRABIT_API_KEY}"
      }
    }
  }
}
```

Restart OpenCode. The tools show up as `jirrabit_getJiraIssue`,
`jirrabit_searchJiraIssuesUsingJql`, and so on — OpenCode prefixes every tool
with the server name.

### Claude Code

```bash
claude mcp add --transport http jirrabit https://jirrabit-mcp.ricardorobles.es/mcp --scope user
```

`--scope user` makes it available in every project; without it, only in the
current one. Confirm with `claude mcp list`, then just ask — the tools appear
as `mcp__jirrabit__getJiraIssue` and friends.

To stop passing credentials on every call, send them once as headers instead:

```bash
claude mcp add --transport http jirrabit https://jirrabit-mcp.ricardorobles.es/mcp --scope user \
  --header "X-Jirrabit-Instance-Url: https://jirrabit.ricardorobles.es" \
  --header "X-Jirrabit-Api-Key: jirrabit-public-demo-token-2026-do-not-use"
```

### Your first call (both clients)

Without headers, every tool call carries its own jirrabit credentials — the
server stores none, so there is nothing to configure and nothing to leak. Pass
the instance and the published demo token on each call:

```json
{
  "instanceUrl": "https://jirrabit.ricardorobles.es",
  "apiKey": "jirrabit-public-demo-token-2026-do-not-use"
}
```

With the headers above, omit both and the call resolves to the same demo.
Explicit arguments always win over headers, so one registration can still reach
a second instance per call — headers pin nothing.

Try asking your agent: *"Who am I on the demo jirrabit?"* (it calls
`getJiraCurrentUser`), then *"List the open issues in the DEMO project."* To
see the same data without an agent, open <https://jirrabit.ricardorobles.es>
and log in as `alice_pm` / `demopass`.

Against your own jirrabit, change `instanceUrl` and use a key from your
profile's **API keys** page. Nothing about your instance is stored here: every
call carries its own credentials, so one deployment serves as many instances as
point at it.

If the tools do not show up, restart the client first — both cache the tool
list at startup. In Claude Code, `claude mcp list` shows each server's status.
A "tool not found" for a `delete*` tool means the operator did not enable the
delete flag on that deployment; reads and non-destructive writes always work.

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

An argument a tool does not declare is an error that lists the ones it does, and
usually names the one you meant. That is deliberate: a misspelled argument used
to be dropped in silence, so `createJiraIssue` reported success and stored no
issue type at all when sent `issueType` instead of `issueTypeName`.

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
| `JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE` | `0` | Acknowledge a default instance on a publicly-reachable listener |
| `JIRRABIT_MCP_ALLOWED_HOSTS` | — | Allowlist of instance hosts. `a.com,b.com`, or `.b.com` for a domain and its subdomains |
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

A default instance is a convenience for a server you run for yourself, and a leak
on one you publish: any caller who supplies no `instanceUrl` — including every
caller whose own key is missing or rejected — is silently served with the
operator's key and walks away with the result.

So the server refuses to start when it has **both** a default instance **and** a
listener strangers can reach: `-transport http` on anything but loopback, which
is the wildcard `:8082`, a public IP, or a container address. It does not object
over stdio, where your client spawned the process and there is only one caller,
nor on a loopback address, which is what a private server behind a same-host
reverse proxy looks like. Set `JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE=1` to override
when you have decided, or bind to `127.0.0.1`. The error is a refusal rather than
a warning because the mistake is invisible until someone else's agent has read
your data.

`JIRRABIT_MCP_ALLOWED_HOSTS` is the control that makes a published server safe;
see [Publishing a shared server](#publishing-a-shared-server).

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

Always on — 114 tools:

| Tool | jirrabit endpoint |
|---|---|
| `getJiraCurrentUser` | `GET /api/v1/me/` |
| `updateJiraCurrentUser` | `PATCH /api/v1/me/` |
| `listJiraProjects` | `GET /api/v1/projects/` |
| `getJiraProject` | `GET /api/v1/projects/{key}/` |
| `listJiraProjectMembers` | `GET /api/v1/projects/{key}/members/` |
| `listJiraEpics` | `GET /api/v1/projects/{key}/epics/` |
| `createJiraEpic` | `POST /api/v1/projects/{key}/epics/` |
| `listJiraTransitions` | `GET /api/v1/statuses/{id}/transitions/` |
| `listJiraLabels` | `GET /api/v1/labels/` |
| `createJiraSavedFilter` | `POST /api/v1/filters/` |
| `listJiraProjectIssues` | `GET /api/v1/projects/{key}/issues/` |
| `getJiraIssue` | `GET /api/v1/issues/{key}/` |
| `getJiraIssueChangelog` | `GET /api/v1/issues/{key}/changelog/` |
| `listJiraIssueAttachments` | `GET /api/v1/issues/{key}/attachments/` |
| `getJiraAttachment` | `GET /api/v1/attachments/{id}/` |
| `addJiraAttachment` | `POST /api/v1/issues/{key}/attachments/` |
| `listJiraNotifications` | `GET /api/v1/notifications/` |
| `markJiraNotificationsRead` | `POST /api/v1/notifications/read/` |
| `listJiraTeams` | `GET /api/v1/teams/` |
| `getJiraProjectActivity` | `GET /api/v1/projects/{key}/activity/` |
| `getJiraProjectSla` | `GET /api/v1/projects/{key}/sla/` |
| `getJiraProjectBurndown` | `GET /api/v1/projects/{key}/burndown/` |
| `getJiraProjectReports` | `GET /api/v1/projects/{key}/reports/` |
| `createJiraStatus` | `POST /api/v1/statuses/` |
| `updateJiraStatusTransitions` | `PATCH /api/v1/statuses/{id}/` |
| `deleteJiraStatus` | `DELETE /api/v1/statuses/{id}/` |
| `createJiraPriority` | `POST /api/v1/priorities/` |
| `deleteJiraPriority` | `DELETE /api/v1/priorities/{id}/` |
| `createJiraIssueType` | `POST /api/v1/issue-types/` |
| `deleteJiraIssueType` | `DELETE /api/v1/issue-types/{id}/` |
| `listJiraCustomFields` | `GET /api/v1/projects/{key}/custom-fields/` |
| `getJiraIssueCustomFields` | `GET /api/v1/issues/{key}/custom-fields/` |
| `setJiraIssueCustomFieldValues` | `PATCH /api/v1/issues/{key}/custom-fields/` |
| `listJiraWebhooks` | `GET /api/v1/projects/{key}/webhooks/` |
| `getJiraProjectWiki` | `GET /api/v1/projects/{key}/wiki/` |
| `createJiraIssue` | `POST /api/v1/projects/{key}/issues/` |
| `cloneJiraIssue` | `POST /api/v1/issues/{key}/clone/` |
| `editJiraIssue` | `PATCH /api/v1/issues/{key}/` |
| `transitionJiraIssue` | `PATCH /api/v1/issues/{key}/` with `status_id` |
| `searchJiraIssuesUsingJql` | `GET /api/v1/search?jql=` |
| `listJiraIssueComments` | `GET /api/v1/issues/{key}/comments/` |
| `addOrEditJiraIssueComment` | `POST /api/v1/issues/{key}/comments/` (create only) |
| `listJiraIssueWorklogs` | `GET /api/v1/issues/{key}/worklogs/` |
| `addOrEditJiraIssueWorklog` | `POST /api/v1/issues/{key}/worklogs/`, `PATCH /api/v1/issues/{key}/worklogs/{id}/` |
| `listJiraIssueLinkTypes` | `GET /api/v1/link-types/` |
| `createJiraIssueLink` | `POST /api/v1/issues/{key}/links/` |
| `getJiraIssueLinks` | `GET /api/v1/issues/{key}/links/` |
| `listJiraIssueWatchers` | `GET /api/v1/issues/{key}/watchers/` |
| `watchJiraIssue` | `POST`/`DELETE /api/v1/issues/{key}/watchers/` |
| `listJiraSprints` | `GET /api/v1/projects/{key}/sprints/` |
| `getJiraSprint` | `GET /api/v1/sprints/{id}/` |
| `createJiraSprint` | `POST /api/v1/projects/{key}/sprints/` |
| `updateJiraSprint` | `PATCH /api/v1/sprints/{id}/` |
| `listJiraSavedFilters` | `GET /api/v1/filters/` |
| `listJiraUsers` | `GET /api/v1/users/search/`, query optional |
| `getJiraUser` | `GET /api/v1/users/{id}/` or `GET /api/v1/users/search/?query=` |
| `listJiraStatuses` | `GET /api/v1/statuses/` |
| `listJiraPriorities` | `GET /api/v1/priorities/` |
| `listJiraIssueTypeMetadata` | `GET /api/v1/issue-types/` |
| `moveJiraIssue` | `POST /api/v1/issues/{key}/move/` |
| `reorderJiraBoardColumn` | `POST /api/v1/projects/{key}/board/reorder/` |
| `bulkUpdateJiraBoard` | `POST /api/v1/projects/{key}/board/bulk-update/` |
| `listJiraPins` | `GET /api/v1/pins/` |
| `pinJiraItem` | `POST /api/v1/pins/` |
| `unpinJiraItem` | `DELETE /api/v1/pins/{id}/` |
| `getJiraIssueTimer` | `GET /api/v1/issues/{key}/timer/` |
| `startJiraIssueTimer` | `POST /api/v1/issues/{key}/timer/start/` |
| `stopJiraIssueTimer` | `POST /api/v1/issues/{key}/timer/stop/` |
| `snoozeJiraIssueNotifications` | `POST /api/v1/issues/{key}/snooze/` |
| `unsnoozeJiraIssueNotifications` | `DELETE /api/v1/issues/{key}/snooze/` |
| `listJiraCommentReactions` | `GET /api/v1/issues/{key}/comments/{id}/reactions/` |
| `reactToJiraComment` | `POST /api/v1/issues/{key}/comments/{id}/reactions/` |
| `removeJiraCommentReaction` | `DELETE /api/v1/issues/{key}/comments/{id}/reactions/{emoji}/` |
| `listJiraIssueBranchLinks` | `GET /api/v1/issues/{key}/branches/` |
| `linkJiraBranchToIssue` | `POST /api/v1/issues/{key}/branches/` |
| `unlinkJiraBranchFromIssue` | `DELETE /api/v1/issues/{key}/branches/{id}/` |
| `listJiraIssueTemplates` | `GET /api/v1/projects/{key}/issue-templates/` |
| `createJiraIssueTemplate` | `POST /api/v1/projects/{key}/issue-templates/` |
| `updateJiraIssueTemplate` | `PATCH /api/v1/projects/{key}/issue-templates/{id}/` |
| `exportJiraIssuesCsv` | `GET /api/v1/projects/{key}/csv-export/` |
| `importJiraIssuesCsv` | `POST /api/v1/projects/{key}/csv-import/` |
| `listJiraBoardViews` | `GET /api/v1/projects/{key}/board-views/` |
| `saveJiraBoardView` | `POST /api/v1/projects/{key}/board-views/` |
| `deleteJiraBoardView` | `DELETE /api/v1/projects/{key}/board-views/{id}/` |
| `listJiraRecentIssues` | `GET /api/v1/recent/` |
| `clearJiraRecentIssues` | `DELETE /api/v1/recent/` |
| `listJiraCommentMentions` | `GET /api/v1/issues/{key}/comments/{id}/mentions/` |
| `createJiraProject` | `POST /api/v1/projects/` |
| `addJiraProjectMember` | `POST /api/v1/projects/{key}/members/` |
| `updateJiraProjectMember` | `PATCH /api/v1/projects/{key}/members/{username}/` |
| `getJiraEpic` | `GET /api/v1/projects/{key}/epics/{id}/` |
| `updateJiraEpic` | `PATCH /api/v1/projects/{key}/epics/{id}/` |
| `updateJiraStatus` | `PATCH /api/v1/statuses/{id}/` |
| `updateJiraPriority` | `PATCH /api/v1/priorities/{id}/` |
| `updateJiraIssueType` | `PATCH /api/v1/issue-types/{id}/` |
| `updateJiraLabel` | `PATCH /api/v1/labels/{id}/` |
| `createJiraCustomField` | `POST /api/v1/projects/{key}/custom-fields/` |
| `updateJiraProjectWiki` | `PUT /api/v1/projects/{key}/wiki/` |
| `createJiraWebhook` | `POST /api/v1/projects/{key}/webhooks/` |
| `updateJiraWebhook` | `PATCH /api/v1/projects/{key}/webhooks/{id}/` |
| `createJiraTeam` | `POST /api/v1/teams/` |
| `updateJiraTeam` | `PATCH /api/v1/teams/{id}/` |
| `listJiraApiKeys` | `GET /api/v1/api-keys/` |
| `createJiraApiKey` | `POST /api/v1/api-keys/` |
| `listJiraAdminUsers` | `GET /api/v1/admin/users/` |
| `createJiraAdminUser` | `POST /api/v1/admin/users/` |
| `updateJiraAdminUser` | `PATCH /api/v1/admin/users/{id}/` |
| `listJiraInvites` | `GET /api/v1/admin/invites/` |
| `createJiraInvite` | `POST /api/v1/admin/invites/` |
| `revokeJiraInvite` | `DELETE /api/v1/admin/invites/{id}/` |
| `getJiraCommentHistory` | `GET /api/v1/issues/{key}/comments/{id}/history/` |
| `archiveJiraIssue` | `PATCH /api/v1/issues/{key}/` with `archived` |
| `startJiraSprint` | `POST /api/v1/sprints/{id}/start/` |
| `closeJiraSprint` | `POST /api/v1/sprints/{id}/close/` |

The three metadata tools exist so an agent can turn a status, priority or issue
type *name* into the numeric id that the write tools require, instead of
guessing. `listJiraUsers` is the same idea for people: with no arguments it
lists everyone, which is how an agent finds a username to assign when it was
given a display name rather than one.

`moveJiraIssue`, `reorderJiraBoardColumn` and `bulkUpdateJiraBoard` take a status
or priority *name* and resolve it for you, so the same question an agent can
always answer — "what are the statuses called here?" — does not have to be asked
before every move. When the name is wrong the error lists the real ones.

The board tools exist because setting a status was possible and *placing* a card
was not: `rank`, the card's index inside its column, had no endpoint, so an issue
an agent moved always landed at the end. They renumber the whole column rather
than nudging a single card, because a column is a dense `0..n-1` run and that is
an invariant rather than a value. `reorderJiraBoardColumn` accepts a shorter list
than the column holds and reads it as "these to the top, the rest keep their
order", which is what a stale view of the board sends. `bulkUpdateJiraBoard`
reports the issues it skipped and why, and refuses `delete` outright: the one
action there that cannot be undone should not be reachable from a call that takes
a list. Use `deleteJiraIssue`, which previews and asks for confirmation.

Opt-in:

| Tool | Enabled by |
|---|---|
| `deleteJiraIssue` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraSprint` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraEpic` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraIssueComment` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `restoreJiraIssueComment` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `updateJiraProject` | `JIRRABIT_MCP_ENABLE_MANAGE` |
| `deleteJiraProject` | `DELETE /api/v1/projects/{key}/` |
| `deleteJiraIssueLink` | `DELETE /api/v1/issues/{key}/links/{id}/` |
| `deleteJiraIssueWorklog` | `DELETE /api/v1/issues/{key}/worklogs/{id}/` |
| `deleteJiraIssueTemplate` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraLabel` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraCustomField` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraWebhook` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraTeam` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraApiKey` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraAttachment` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `removeJiraProjectMember` | `JIRRABIT_MCP_ENABLE_DELETE` |
| `deleteJiraSavedFilter` | `JIRRABIT_MCP_ENABLE_DELETE` |

With both flags on, 126 tools are registered.

Every delete previews before it acts: the first call returns what would be
removed, by kind and with real counts, plus a confirmation token, and changes
nothing. That includes the ones whose subject row is not the risk — deleting a
sprint says how many issues are about to drop out of it, deleting a label says how
many issues will lose it, deleting a team names the people who stop being
notified. Where a reversible option exists the preview names it: `archiveJiraIssue`
over `deleteJiraIssue`, `active: false` over deleting a webhook, recreating over
deleting a saved filter.

Still missing, because jirrabit exposes no endpoint for them: boards as
configurable entities, versions, components, and entity properties. They will
appear as the API grows; see the tool surface in [AGENTS.md](AGENTS.md).

Seven Atlassian arguments are refused rather than ignored, because jirrabit cannot
honour them and a silent no-op is worse than an error: `fields` and `expand` on
`getJiraIssue`, `visibilityType` and `visibilityValue` on
`addOrEditJiraIssueComment`, and `newEstimate`, `adjustEstimate` and `reduceBy`
on `addOrEditJiraIssueWorklog` — estimates live on the issue, so those three are
routed to `editJiraIssue` instead. Each error names what is missing.

### Multi-tenancy, verified

A shared deployment is the normal case here, so it is tested rather than
asserted. `cmd/isolationtest` runs against two live instances over a single
stdio session and checks that:

- each instance answers for itself and sees only its own projects;
- a key presented to the instance it does not belong to is rejected with a 401
  naming authentication, not served with the other tenant's data;
- a corrupted key is rejected outright and never falls back to the server's
  default instance;
- interleaved A, B, A, B calls on one connection stay separated — the ordering
  is the point, since a client pool keyed on the wrong thing passes every
  non-interleaved test.

`cmd/multitenancy` additionally covers a key with no `instanceUrl`, a loopback
URL, and `cloudId` being accepted and ignored. The loopback rejection is
deliberate: a shared server must not let a caller reach ports on the host it
runs on.

### One known gap

`LinkOut.created_by` is still a numeric user id while `source` and `target` are
keys. It is left as it is because changing it would break a second field for no
gain, but it is the one identifier in the payload that a caller cannot act on.
`getJiraUser` is the tool that turns it into a name.

## Verifying a deployment

```bash
go test ./...
go vet ./...
gofmt -l .                # must print nothing

# The server's own prose has to match its own tool list, and it also prints the
# count. Needs no jirrabit, so it runs in CI on every push. 110 tools registered
# without a flag.
go run ./cmd/flowtest -phantoms-only -server ./bin/jirrabit-mcp

# End to end against a real jirrabit. Creates one issue, so use a scratch one.
JIRRABIT_URL=… JIRRABIT_API_KEY=… go run ./cmd/smoke -server ./bin/jirrabit-mcp

# Everything smoke does, plus every assertion about response shapes, the
# write/read round trip, the error paths, multi-tenant isolation and the opt-in
# gates. 245 checks with the flags on, which is the number that matters: several
# of them assert the *opposite* answer when a flag is off, so run it both ways.
JIRRABIT_URL=… JIRRABIT_API_KEY=… \
  JIRRABIT_MCP_ENABLE_DELETE=1 JIRRABIT_MCP_ENABLE_MANAGE=1 \
  go run ./cmd/flowtest -server ./bin/jirrabit-mcp

# Two instances over one connection, with project isolation and the
# rejection paths (bad key, key without a URL, loopback URL).
go run ./cmd/multitenancy -server ./bin/jirrabit-mcp \
  -aURL … -aKey … -aProject … -bURL … -bKey … -bProject …

# The cross-auth check, and the one to run before trusting a shared deployment.
# Proves that A's key cannot read B, that a corrupted key never falls back to
# the default instance, and that interleaved A,B,A,B calls on one connection
# stay separated. Needs two instances and two keys, so it is not in CI.
go run ./cmd/isolationtest -server ./bin/jirrabit-mcp \
  -aURL … -aKey … -aProject … -bURL … -bKey … -bProject …

# flowtest and cmd/smoke discover the project key from the instance, so they
# work against any jirrabit and not only the demo. Pass -project to override.

# Just list the tools a server registers.
JIRRABIT_URL=… JIRRABIT_API_KEY=… go run ./internal/probe ./bin/jirrabit-mcp
```

## Publishing a shared server

So that anyone can use their own jirrabit through **your** URL, without
downloading or building anything: run the streamable HTTP transport, put it
behind TLS, and let each caller name their own instance.

```bash
docker run -d --name jirrabit-mcp -p 127.0.0.1:8082:8082 \
  -e JIRRABIT_MCP_TRANSPORT=http \
  -e JIRRABIT_MCP_ALLOWED_HOSTS='.example.com' \
  ghcr.io/rroblf01/jirrabit-mcp:latest
```

The image is published to GitHub Container Registry on every release, for
`linux/amd64` and `linux/arm64` — 17 MB, built `FROM scratch`, running as
`nobody`. Pin a version tag rather than `latest`. `-p 127.0.0.1:8082:8082` binds
the published port to loopback on purpose: the reverse proxy on the same host
reaches it, and nothing else does.

To build it yourself instead, `docker build -t jirrabit-mcp .` from a checkout.

> The first published package is **private** by default even when the repository
> is public, so `docker pull` fails for everyone but you until you set it to
> public once: the package's page on GitHub → Settings → Change visibility.

A client then points at your endpoint and supplies its own credentials on every
call — no install, no account with you:

```json
{
  "mcp": {
    "jirrabit": {
      "type": "http",
      "url": "https://mcp.example.com/mcp"
    }
  }
}
```

```
"arguments": { "instanceUrl": "https://mi.empresa.com", "apiKey": "…" }
```

Nothing about the caller's instance or key is stored here. The pool caches a
client per `url|sha256(key)` for ten minutes so connections are reused, and drops
it afterwards; the key itself is never a map key, never logged, and never
written to disk.

Four things to decide before you publish.

**The endpoint has no authentication of its own.** Authorisation happens per
tool call through the caller's `apiKey` — this server never checks whether the
*caller* is entitled to make calls. So put it behind something that does: a
reverse proxy with rate limiting, a shared secret, or an identity-aware proxy.
An open endpoint on the public internet can be used by anyone to spend your
outbound bandwidth.

**Set `JIRRABIT_MCP_ALLOWED_HOSTS`.** A server that will fetch any URL it is
handed is a proxy, and one on a host with a private network attached can be
pointed at that network. The built-in guard refuses loopback and link-local
addresses (so not `169.254.169.254`) and never follows redirects, which closes
the obvious pivots, but a caller can still name `10.0.0.0/8` and read the answer
back through a tool result. The allowlist is exact by default — `a.com` matches
only `a.com`, and `a.com.evil.net` is not a match — and `.example.com` matches
that domain and every subdomain. Leave it unset only for a private server on a
trusted network.

**Leave `JIRRABIT_URL` unset.** It is refused outright unless you also set
`JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE=1`, and a shared server has no business
having one. Anyone whose own key is missing would otherwise be served yours.

**Require HTTPS at the proxy.** The transport speaks plain HTTP; terminate TLS in
front of it. Also forward the client's address if you rate limit, and give the
container a read-only filesystem with no secrets mounted.

### Deploying on a VPS

1. `docker compose up -d` — or run the binary behind your own TLS terminator.
2. Put it behind a reverse proxy if it should not be reachable directly. The
   streamable HTTP endpoint is unauthenticated: authorisation happens per tool
   call through the caller's `apiKey`, but nothing stops an unauthenticated
   client from making calls. Anyone who reaches the endpoint can consume it.
3. If jirrabit is on the same host and reachable by name, set `JIRRABIT_URL` to
   it. On a publicly-reachable listener that also needs
   `JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE=1`, or the server refuses to start. If
   jirrabit is in a container, add `host.docker.internal` to jirrabit's
   `JIRRABIT_ALLOWED_HOSTS`, or Django answers 400.
4. Pin to a release rather than tracking `latest`.

## Requirements

Go 1.27 to build. One dependency: `github.com/mark3labs/mcp-go`. Runtime is a
single static binary, or the container image.

## Changelog

[`CHANGELOG.md`](CHANGELOG.md), in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
format. The newest release is 1.2.1. Work that is not released yet sits under
[Unreleased](CHANGELOG.md#unreleased), and a section that has been published is
never edited afterwards — two tests in `cmd/jirrabit-mcp` hold that line, because
a changelog that over-claims still reads perfectly well.

## License

MIT. Not affiliated with, endorsed by, or sponsored by Atlassian.
