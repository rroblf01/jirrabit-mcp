# Changelog

All notable changes to jirrabit-mcp are recorded in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the project uses [semantic versioning](https://semver.org/spec/v2.0.0.html). A
release is cut by publishing a GitHub release, which is also what pushes the
container image to `ghcr.io/rroblf01/jirrabit-mcp`.

The version a running server reports — in the `initialize` response, which is
how you tell two builds apart — is stamped at image build time from the release
tag. `cmd/jirrabit-mcp` falls back to `1.0.0` when built from a checkout.

## [Unreleased]

### Added

- **Unknown arguments are rejected, with the valid ones listed** and the closest
  match suggested. Found by running a real 9B model against a real deployment:
  `createJiraIssue` was quietly discarding `priorityId`, a field Atlassian's own
  tool has, so an agent trained on it produced an issue with no priority and no
  explanation. Turning the check on surfaced it at once, and `flowtest` failed 26
  checks until the schema was fixed rather than the check relaxed.
- `priorityId`, `statusId` and `sprintId` are now first-class arguments on
  `createJiraIssue` and `editJiraIssue`. jirrabit's API accepted all three and
  the MCP only reached them through the free-form `fields` object, so nothing
  told an agent they existed. Sending one in both places is not an error: the
  named argument wins.

### Fixed

- The server instructions promised a default instance on servers that have none,
  which is every shared deployment and therefore the main one. The sentence is
  now chosen from the server's own configuration, so an agent is told either to
  pass both arguments or that it may omit them, and is never told a promise the
  server cannot keep.
- The README documents the binary at `/jirrabit-mcp` rather than
  `/usr/local/bin/jirrabit-mcp`. The image is built `FROM scratch`, so there is
  no directory tree beyond what the image puts there, and no shell to
  `docker exec` into.

## [1.0.0] - 2026-09-27

The first release. The image published on this tag is what
`https://jirrabit-mcp.ricardorobles.es/mcp` is running.

### Added

- **24 tools, always on**, using Atlassian's Jira names, parameter names and
  payload shapes so an agent trained on the official Atlassian server needs no
  new vocabulary: `getJiraIssue`, `searchJiraIssuesUsingJql`, `createJiraIssue`,
  `editJiraIssue`, `transitionJiraIssue`, `addOrEditJiraIssueComment`,
  `createJiraIssueLink`, `getJiraIssueLinks`, `listJiraIssueLinkTypes`,
  `addOrEditJiraIssueWorklog`, `listJiraIssueWorklogs`, `watchJiraIssue`,
  `getJiraCurrentUser`, `getJiraUser`, `listJiraProjects`, `listJiraStatuses`,
  `listJiraPriorities`, `listJiraIssueTypeMetadata`, `listJiraSprints`,
  `createJiraSprint`, `updateJiraSprint`, `getJiraSprint`,
  `listJiraSavedFilters` and `listJiraIssueComments`.
- **3 tools behind a flag**, off by default: `deleteJiraIssue` and
  `deleteJiraSprint` with `JIRRABIT_MCP_ENABLE_DELETE=1`, and
  `updateJiraProject` with `JIRRABIT_MCP_ENABLE_MANAGE=1`. Visibility only —
  jirrabit still authorises the action.
- **Multi-tenancy**: every tool takes `instanceUrl` and `apiKey`, so one
  deployed server serves any number of jirrabit instances and stores none of
  their credentials. `cloudId` is accepted and ignored, because Atlassian's tools
  require it and an agent will send it.
- **Two transports**: stdio, for a client that runs the binary, and stateless
  streamable HTTP on `/mcp`, for a server behind a load balancer.
- **`JIRRABIT_MCP_ALLOWED_HOSTS`**, an operator allowlist of the instances the
  server will call. Matching is exact per entry, except that a leading dot
  matches a domain and its subdomains. Unset accepts any host that is not
  loopback or link-local, which is the right default on a trusted LAN and the
  wrong one on the internet.
- **Verification commands**: `cmd/smoke` drives the server end to end over
  stdio, `cmd/flowtest` asserts 64 response shapes and error paths,
  `cmd/isolationtest` proves two instances stay separated over one connection,
  and `cmd/multitenancy` does the same interactively.
- **A container image**, 17 MB, `FROM scratch`, `linux/amd64` and `linux/arm64`,
  published to `ghcr.io/rroblf01/jirrabit-mcp` when a release is published.

### Security

- **A default instance on a publicly-reachable listener is refused at startup.**
  With `JIRRABIT_URL` set, any caller who supplies no `instanceUrl` — including
  every caller whose own key is missing or rejected — would be served the
  operator's key and could read the operator's data. The server now refuses to
  start in that combination unless `JIRRABIT_MCP_ALLOW_DEFAULT_INSTANCE=1` is
  set. Over stdio, or bound to loopback, the default is harmless and is not
  questioned.
- **The target guard refuses loopback and link-local addresses** and never
  follows redirects, which closes the obvious SSRF pivots including the
  `169.254.169.254` metadata endpoint. A consequence worth knowing: a host-side
  client cannot reach an instance at `localhost`, and has to use a name the
  server can resolve.
- **Keys are never logged and never a cache key.** Clients are pooled on
  `url|sha256(key)`, swept after ten minutes unused, and a rotated key stops
  being used without a restart.
- All 27 tools carry read-only, destructive, idempotent and open-world hints,
  because agents read them before calling.

### Fixed

- The server instructions and every tool description were pointing at
  `transitionJiraIssue` before it existed, which made an agent call a tool that
  was not registered and stop. `cmd/flowtest -phantoms-only` now scans the prose
  and fails on any tool-shaped name that is not registered; it runs in CI and
  needs no jirrabit.
- Write tools were silently dropping their `fields` argument.
- The container healthcheck sent a `GET`, which the streamable HTTP transport
  answers by opening an event stream that never completes, so it verified that a
  port was open and nothing else. It now sends a real `initialize` and derives
  its URL from `JIRRABIT_MCP_ADDR` and `JIRRABIT_MCP_PATH`.
- `flowtest` and `cmd/smoke` asserted a literal `DEMO` project key and passed on
  the instance they were written against while failing 34 checks on any other.
  Both discover the project from `listJiraProjects` now.
- A wildcard listen address logged as `http://:8082`, which reads like a broken
  URL rather than the wildcard bind it is.

### Known limitations

- `createJiraProject` is not exposed yet. `searchJiraIssuesUsingJql` is, and
  works; it is jirrabit's REST API that still lacks a couple of endpoints the
  Atlassian tool set implies, not this server.
- `updateJiraSprint` does not close a sprint, because jirrabit's close carries
  unfinished issues to another sprint and no endpoint does only that. The tool
  description says so rather than letting an agent assume.
- An issue's detail payload does not include its worklogs. Read them with
  `listJiraIssueWorklogs`.
- Outbound HTTP is not dispatched anywhere: this server calls jirrabit's REST API
  and nothing else.

[Unreleased]: https://github.com/rroblf01/jirrabit-mcp/commits/main
[1.0.0]: https://github.com/rroblf01/jirrabit-mcp/releases/tag/v1.0.0
