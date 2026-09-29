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

## [1.5.0] - 2026-09-29

### Added

- Per-call credentials over HTTP headers: `X-Jirrabit-Instance-Url` and
  `X-Jirrabit-Api-Key` are accepted as an alternative to `instanceUrl`/`apiKey`
  on every tool call. Explicit arguments still win, so one client registration
  can pin one instance via headers and still reach others per call; a header
  URL gets exactly the caller-supplied treatment (SSRF guard, allowlist), never
  the operator-default exemption. Two custom headers rather than
  `Authorization: Bearer`, so neither half can be mistaken for an OAuth flow.
  No headers exist over stdio, where the operator's environment stays the way
  to name a default.

## [1.4.0] - 2026-09-29

### Added

- `timeRemainingMinutes` on `createJiraIssue`, mirroring `editJiraIssue`. The
  Django create endpoint always accepted it, but the tool did not declare it,
  so the strict checker refused it instead of silently dropping it — which is
  exactly what the checker is for, and how the gap was found: a live run
  against the previous deployment failed loudly rather than creating an issue
  with the wrong remaining estimate.

## [1.3.0]

### Added

- `updateJiraCurrentUser`, with `PATCH /api/v1/me/` underneath it: the caller's
  own display name, email, job title, timezone, language, palette,
  notification-email switch, muted notification kinds and avatar. jirrabit had
  only the profile form for this, so an agent could read who it was and change
  nothing about itself. Username, password and privilege flags are deliberately
  out of reach — the first is the login, the second rotates through the web
  flow, the third belongs to `updateJiraAdminUser` — and the muted-kinds list is
  replaced whole, so the tool says to read the profile first.
- `updateJiraIssueTemplate`, with `PATCH
  /api/v1/projects/{key}/issue-templates/{id}/` underneath it: the name, type,
  default summary, default description, default priority and labels of a
  template. Create, list and delete existed while a typo in a default could only
  be fixed by deleting the template and recreating it. A default priority is
  removed with `clearPriority`, because a null and an absent `priorityId` arrive
  identically and only one of them can mean "remove it".
- Worklog correction, end to end: `PATCH
  /api/v1/issues/{key}/worklogs/{id}/` edits minutes, comment and when the work
  happened, moving the issue's totals by the delta under the same row lock as
  the log and unlog paths, and `POST` accepts `started` to backdate an entry.
  `addOrEditJiraIssueWorklog` finally honours the second half of its name —
  its description promised editing while the handler refused it — and `started`
  with it, so "I logged 3h, it was 2h, and it was Tuesday" is one call. The
  estimate arguments stay refused, but the message now routes to `editJiraIssue`,
  whose `estimateMinutes` was always writable and which gains
  `timeRemainingMinutes`: the old text claimed both were read-only, which
  stopped being true without anyone updating it.
- `cloneJiraIssue`, with `POST /api/v1/issues/{key}/clone/` underneath it:
  summary, description, type, priority, assignee, labels, epic, story points,
  estimate and due date, with the caller as reporter. The same field set as
  the web UI's clone, so the two cannot disagree about what a copy means.
  Subtasks copy one level only when `includeSubtasks` says so, and comments,
  history, attachments, links, time and the archived flag never copy.
- Project analytics, read-only: `getJiraProjectSla`,
  `getJiraProjectBurndown` and `getJiraProjectReports`, over
  `GET /api/v1/projects/{key}/sla|burndown|reports/`. The three pages answer
  what standup asks — what is stuck, how the sprint is going, how fast the
  team ships — and recomputing them client-side from changelog pages is the
  kind of work that drifts between callers. The aggregation mirrors the web
  views query for query, and the payloads carry data rather than markup: no
  SVG coordinates, and future burndown days carry null rather than zero, so a
  client that plots null as zero does not draw a cliff that is not there.

## [1.2.1] - 2026-09-28

### Fixed

- **The opt-in exemption list in `cmd/flowtest` named eight tools when the delete
  flag gates eighteen.** 1.2.0 added ten more opt-in tools and left the list where
  it was, so it covered fewer tools than the flag did. Nothing failed, and that is
  the problem: the check only looks for a tool name in the instructions or a
  description, and the prose happened not to mention any of the ten. So the list
  was a false negative waiting for the next person to write "prefer
  `deleteJiraWebhook` with `active:false`" in a tool description, which reads as a
  phantom-tool bug and is really an incomplete list. The comment above the list
  already predicted this, and the fix is the one the comment describes: all
  eighteen together, so the next opt-in tool is added in the same place.
- **Three duplicate rows in the README's opt-in table.** `deleteJiraIssue`,
  `deleteJiraSprint` and `deleteJiraEpic` were each listed twice, left over from
  filling in tools that were already there. The table still rendered, and a
  reader would read it as emphasis.

## [1.2.0] - 2026-09-28

### Upgrading

- **`deleteJiraSprint` and `deleteJiraEpic` now take two calls.** Both were one
  call, and both are opt-in tools whose whole point is to be hard to use by
  accident. A client that called either once and expected a deletion now gets a
  preview and a `confirmationToken` back, and has to call again with it. The
  second call is unchanged in shape: same arguments, plus `confirm`. If you drive
  these from a script, this is the one thing in the release that needs a code
  change on your side.
- `jirrabit-mcp` itself needs no change to be upgraded: a release publishes the
  container image, so `docker pull` and a restart is the whole procedure. The
  new tools do need a jirrabit new enough to serve their endpoints — the tool
  count is 110 rather than 76, and the endpoints behind the 34 newest were added
  in step with them.

### Added

- The rest of the surface, found by diffing all 118 endpoint methods against the
  86 tools that existed: 34 were reachable over HTTP and not over MCP, or existed
  only in the web UI. `createJiraProject`, `addJiraProjectMember`,
  `updateJiraProjectMember`, `removeJiraProjectMember`, `getJiraEpic`,
  `updateJiraEpic`, `updateJiraStatus`, `updateJiraPriority`,
  `updateJiraIssueType`, `updateJiraLabel`, `createJiraCustomField`,
  `updateJiraProjectWiki`, `createJiraWebhook`, `updateJiraWebhook`,
  `createJiraTeam`, `updateJiraTeam`, `deleteJiraTeam`, `listJiraApiKeys`,
  `createJiraApiKey`, `deleteJiraApiKey`, `listJiraAdminUsers`,
  `createJiraAdminUser`, `updateJiraAdminUser`, `listJiraInvites`,
  `createJiraInvite`, `revokeJiraInvite`, `getJiraCommentHistory`,
  `deleteJiraLabel`, `deleteJiraCustomField`, `deleteJiraWebhook`,
  `deleteJiraAttachment`, `deleteJiraSavedFilter`, plus
  `exportJiraIssuesCsv`, `importJiraIssuesCsv`, `listJiraBoardViews`,
  `saveJiraBoardView`, `deleteJiraBoardView`, `listJiraRecentIssues`,
  `clearJiraRecentIssues` and `listJiraCommentMentions`. 110 tools are registered
  without any flag and 126 with both on.
  - The creates had arrived before the updates, which left an agent able to add a
    status and not able to fix a typo in its name without the web UI. That is the
    kind of gap a diff finds and a release note does not.
  - `listJiraInvites` strips the token from every row as well as relying on the
    endpoint to omit it, so the guarantee does not depend on one response schema.
  - `listJiraCommentMentions` answers "did anyone see what I asked", which was a
    real question with no answer. Naming somebody is public in the comment body,
    so everyone who can see the issue sees the list; the read timestamps go only
    to the comment's author and the people mentioned.
- Board placement, which no tool could do: `moveJiraIssue` (status and position,
  either or both), `reorderJiraBoardColumn` (the whole column, as the board's own
  drag sends it) and `bulkUpdateJiraBoard` (one field across many issues,
  reporting which ones it could not change). The gap was not that an agent could
  not read a board — JQL lists statuses — but that it could not *place* anything:
  `PATCH` set a status and `rank`, the card's index inside its column, had no
  endpoint at all, so every issue an agent touched landed at the end of a column.
  These route through the same helpers a drag uses, which renumber the whole
  column, because `rank` is an invariant rather than a field. Bulk `delete` is
  refused with a pointer at `deleteJiraIssue` instead: the only action there that
  cannot be undone should not be reachable from a call that takes a list.
- Personal state, all of which existed in the UI with no HTTP path: `listJiraPins`,
  `pinJiraItem`, `unpinJiraItem`, `getJiraIssueTimer`, `startJiraIssueTimer`,
  `stopJiraIssueTimer`, `snoozeJiraIssueNotifications`,
  `unsnoozeJiraIssueNotifications`, `listJiraCommentReactions`,
  `reactToJiraComment`, `removeJiraCommentReaction`, `listJiraIssueBranchLinks`,
  `linkJiraBranchToIssue`, `unlinkJiraBranchFromIssue`, `listJiraIssueTemplates`,
  `createJiraIssueTemplate` and `deleteJiraIssueTemplate`. An agent could read an
  issue and change an issue and still had no way to answer "what am I working on
  right now". Stopping a timer returns the work log it minted, so the time is
  visible to the rest of the team rather than only in the answer. The reaction set
  is fixed, and checked before the request, because jirrabit validates it on write
  and an agent picking its own emoji would otherwise spend a call learning that.
- 29 checks in `cmd/flowtest` for the above, including that a card moved to a named
  status really lands in that column, that `reorderJiraBoardColumn` refuses a
  repeated key, that a second timer on one issue is refused rather than moving the
  first, and that the template delete previews, leaves the row in place, applies on
  confirmation, and cannot then be replayed.
- `createJiraStatus`, `createJiraPriority`, `createJiraIssueType`,
  `updateJiraStatusTransitions`, and a delete for each of the three. All
  superuser-only, matching the web UI's workflow editor, and each delete refused
  with a 409 while any issue still uses the value — jirrabit will not strip a
  status from every issue that had it.
- `listJiraCustomFields`, `getJiraIssueCustomFields` and
  `setJiraIssueCustomFieldValues`. A custom field's slug is the key its values are
  stored under, and an unknown slug is refused rather than written: a typo would
  leave a value no definition describes and nothing would ever read it.
- `listJiraWebhooks` and `getJiraProjectWiki`.
- 13 checks in `cmd/flowtest`, including one asserting that a new status is not
  sent to the front of the board, and one that clearing a transition list warns
  that it *opened* the workflow rather than closing it.

### Changed

- `deleteJiraSprint` and `deleteJiraEpic` now preview and confirm like every other
  irreversible tool. Both were one call, which broke the rule that a delete asks
  twice — and the sprint's own row was never the risk, so the preview now counts
  the issues about to drop out of it and the epic's counts the issues that lose
  their grouping. `deleteJiraEpic`'s description previously argued that keeping the
  issues made it safe; that is true and it is also irreversible for the epic.

### Fixed

- **`removeJiraProjectMember` and `deleteJiraSavedFilter` were registered by
  default.** Both are irreversible and both already went through the two-step
  confirmation, so the `JIRRABIT_MCP_ENABLE_DELETE` flag should have covered
  them. They are opt-in now, and the README's tool tables are checked against
  what the server actually registers rather than maintained by hand — a release
  ago the same table claimed a count its rows did not add up to, and three
  registered tools were never listed at all.
- **A duplicate on a unique column was a 500 with a traceback.** `Team.slug` is
  unique and neither the create nor the rename checked it, so a second team with
  the same slug reached the insert and came back as an `IntegrityError`. The web
  UI's form never showed it, which is how it survived: the API was the only way to
  reach it. Both are a 409 with a readable reason now, and a team may still be
  renamed to its own slug — the check is for a collision, not for sameness.
- **An over-long name was a 500 with a traceback.** The create and patch schemas
  carried no length constraints, so a value longer than its column reached
  Postgres and came back as a `DataError` — a traceback with `DEBUG=1` and an
  opaque 500 without it, for what is a typo. The limits are now declared, copied
  from each column, and they are not uniform: `Priority.name` is `varchar(20)`
  while `Status.name` is 40, which is exactly the sort of thing a caller
  discovers by hitting it. A test walks ten endpoints and pins both the status
  code and the absence of a traceback.
- A new status defaulted to `order: 0`, which put its column at the **front** of
  the board and silently reordered every existing column of a live project.
  Omitting the argument now appends.

- `getJiraIssueChangelog` and `getJiraProjectActivity`, kept as two tools because
  they answer different questions and an agent that conflates them draws a wrong
  conclusion either way. The changelog is field-level and **incomplete** — jirrabit
  writes a history row from only two places — and both descriptions say so, since
  silence in a changelog that looks authoritative is the dangerous kind of
  incomplete. The activity feed is signal-driven, so it has no gaps, but carries
  no before/after values.
- `listJiraIssueAttachments`, `getJiraAttachment` and `addJiraAttachment`.
  Attachments were fully implemented in the web UI and absent from the API, so an
  agent working from a screenshot or a spec had no route at all. The upload takes
  base64 because a JSON call cannot carry a file, and the response is a `data:`
  URL because jirrabit stores attachments in the database and there is no file on
  a server to point at.
- `listJiraNotifications` and `markJiraNotificationsRead`, both scoped to the API
  key rather than to a user argument.
- `listJiraTeams`, which is what a `@team:slug` mention resolves to. Without it a
  mention was a string an agent could neither verify nor expand.
- 12 checks in `cmd/flowtest` for the above, including one asserting the
  attachment listing carries no `dataUrl` key at all.

### Fixed

- The attachment listing reported `"dataUrl": ""` on every row. An empty string
  is not the same claim as an absent field: the first says the file has no
  contents and the second says they were not asked for. The tag is `omitempty`
  now — the same mistake the shaper used to make with `components`.

- `listJiraEpics`, `createJiraEpic` and `deleteJiraEpic`. JQL could filter on
  `epic` from its first release while nothing could read or write an epic, so a
  search returned issues whose epic the caller could neither learn nor change.
- `listJiraProjectMembers`, with each user's role and which of them is the lead.
  jirrabit has always rejected an assignee who is not a project member, and the
  list of who is a member was only reachable through the web UI — a rule a
  client could not satisfy. Together with the missing user directory, that is
  why assigning was a guess.
- `listJiraTransitions`, which answers what a work item in a given status may
  legally move to. Previously the only way to learn a workflow was to make a
  call and read the 400, once per transition, and the error named the problem
  without naming the way out.
- `listJiraLabels` and `createJiraSavedFilter`. Labels were readable and
  unwritable — the whole of the free-form surface was missing, so an agent could
  find every issue tagged `urgent` and could not tag anything.
- `parent`, `epicId`, `labels` and `estimateMinutes` as named arguments on
  `createJiraIssue` and `editJiraIssue`, rather than reachable only through the
  free-form `fields` object. 1.1.0 made `priorityId`, `statusId` and `sprintId`
  first-class for exactly this reason, and these are the same class of field:
  an agent trained on Atlassian sends them without being told.
- `archived`, `epic` and `remainingEstimateSeconds` now appear in the Jira-shaped
  issue payload. `archived` in particular: an agent that archived something
  could not read back that it was archived.

### Fixed

- **The free-form `fields` object was unusable for every aliased key.** The
  duplicate check fired on a static list of the arguments the tool *has* rather
  than the ones the caller *sent*, so `fields: {"story_points": 5}` was rejected
  as a duplicate of `storyPoints` whether or not `storyPoints` was in the call —
  and the tool's own description advertises that object. The strict-argument
  check could not see it, because `fields` is a declared argument and its
  contents were never compared with the rest of the call. Six unit tests and a
  flowtest check now cover it.
- Setting `labels` replaces the whole set rather than adding to it, which is what
  a PATCH means. Reading it as "add these" would have left no way to remove a
  label, and the board grew a `label_remove` action because the API had no way to
  express it.

- **Irreversible operations now take two calls.** `deleteJiraIssue`,
  `deleteJiraProject`, `deleteJiraIssueWorklog`, `deleteJiraIssueLink` and
  `closeJiraSprint` do nothing on the first call: it reports what the operation
  would cost — comments, worklogs, attachments, links, subtasks, watchers, and
  for a project its issues, members, sprints and epics — and returns a
  `confirmationToken`. The same call with `confirm` set to that token performs
  it.

  The token is `HMAC-SHA256` over the operation, the target, the instance, a hash
  of the API key and **a digest of the preview**, with a two-minute expiry, keyed
  by `HKDF` over the API key itself. Binding the digest is what makes it a
  confirmation rather than a delay: if the target changed between the two calls,
  the token no longer verifies and the call is refused instead of deleting
  something nobody looked at. Because the key is per instance, a token cannot be
  replayed against a second deployment, and rotating a key invalidates the
  outstanding ones.

  **What it does not do is obtain a human's consent**, and the tool descriptions
  and server instructions say so plainly. An agent that calls the tool twice
  without asking has deleted something. What the first call buys is that the
  consequences are visible before the irreversible step, which the previous
  single-call delete never gave anybody.
- `archiveJiraIssue`, and a reversible alternative named in every delete preview.
  Archiving needed fixing before it could be offered: `Issue.archived` existed
  with a help text promising it was "hidden from default views", and it was
  honoured in two views out of seven — not in the list endpoint, not in search,
  not in JQL — and nothing anywhere set it back to false.
- `deleteJiraIssueComment` and `restoreJiraIssueComment`. jirrabit soft-deletes
  comments, so these are one call each with the inverse named in the response,
  deliberately outside the two-step group: a confirmation token in front of an
  undo button is theatre.
- `startJiraSprint` and `closeJiraSprint`. Closing carries unfinished issues to
  another sprint or back to the backlog, which is why it is in the two-step
  group; starting loses nothing, so it is one call.
- `deleteJiraIssueWorklog` and `deleteJiraIssueLink`. A wrongly-created link was
  permanent through the API: it could be added and read but never removed.
- 56 checks in `cmd/flowtest`, including one that asserts the *first* call of
  every two-step tool leaves the row readable. A delete tool that returned a
  well-formed preview while having already deleted would pass any check that only
  reads the response.

### Changed

- **`addOrEditJiraIssueComment` edits for real.** It refused `commentId` for a
  release with a message naming the missing endpoint; jirrabit has
  `PATCH /issues/{key}/comments/{id}/` now, so the refusal is gone and the tool
  edits. The worklog one still refuses `worklogId`, and its message now points at
  `deleteJiraIssueWorklog` plus a fresh log as the way to correct an entry.
- `IssueFields` carries `archived` through, so an agent that archived something
  can read back that it is archived.
- The confirmed call is not retried. The retry loop is right for reads and wrong
  here in a way that had already bitten: a committed delete whose reply was lost
  got a 404 on the retry, and the agent was told a successful operation had
  failed. Not retrying does not make the case clean, it makes it honest.


### Added

- **`listJiraUsers`**: the user directory, and with no arguments it lists
  everyone. This was the gap that made assigning an issue a guess. Every other
  way of naming a person needed the name first — `getJiraUser` rejects an empty
  one, and `resolveAssignee` had nothing to resolve from — so an agent handed
  "assign this to Bob" could not find `bob_dev`. jirrabit's search endpoint
  documents the empty query as the picker case; no tool reached it, because both
  callers passed a non-empty one. A query narrows on username, display name or
  email, and the server instructions now say to call it before `assignee`.
- **`getJiraProject`**, for reading one project instead of listing every one and
  picking from the result. The instructions previously told agents no such tool
  existed.
- **`listJiraProjectIssues`**, for the issues of one project with optional exact
  `status` and `assignee` filters. The endpoint existed and no tool called it, so
  the only route to a project's issues was JQL.
- **`listJiraIssueWatchers`**. `watchJiraIssue` could add and remove a watcher
  but no tool could read the set, so the tool could change something it had no
  way to observe.
- 20 checks in `cmd/flowtest`, one per defect below plus the four new tools. The
  point of each is that the previous 64 passed while the defect was live.

### Fixed

- **`fields.issuetype.id` was always empty on a real response.** The DTO tagged
  the field `type_id` while jirrabit emits `issue_type_id`, so the value never
  decoded and `intToID(0)` produced `""`. The fields were written before
  jirrabit's `IssueOut` carried them, and the tag guessed the shape's name
  instead of the API's. The existing shaper test passed throughout, because it
  builds the struct by hand and never goes through a JSON tag; the new test
  unmarshals, which is the only way to see this.
- **An issue claimed it had no components.** `components` was declared on the
  DTO and rendered on every issue as `[]`, because jirrabit has no such field at
  all. Omitted from the payload instead: an empty list is an answer, and jirrabit
  was never asked.
- **Four worklog arguments were accepted and thrown away.** `started`,
  `newEstimate`, `adjustEstimate` and `reduceBy` passed the strict check because
  they were declared, and the handler sent only `minutes` and `comment`. "Log 2h
  against Tuesday and reduce the remaining estimate by 2h" returned success, with
  an entry timestamped now and an untouched estimate. All four are now refused
  before anything is written, naming the API field that would have to exist. This
  is the blind spot 1.1.0's argument checking was built to close, and the check
  only sees arguments the schema does not declare.
- **Comment visibility was accepted and dropped.** `visibilityType` and
  `visibilityValue` produced a public comment with nothing saying it was public.
  jirrabit has no groups or roles, so this is a permanent absence rather than a
  pending endpoint, and it is now refused.
- **`getJiraIssue` accepted `fields` and `expand` and ignored both.** An agent
  asking for a narrowed read got the whole issue and could not tell. Neither is
  declared now, so both are refused.
- **`listJiraSprints` truncated silently at jirrabit's default page size** and
  returned a bare `{"projectKey", "values"}` with no count and no
  `nextPageToken`. A project with more sprints than one page held looked like the
  end of the data. It now takes `startAt`/`maxResults`/`nextPageToken` and
  returns the standard envelope. Its registration also pointed at `SprintArgs`
  while the handler bound `ListSprintsArgs`, so the paging arguments were
  rejected as unknown — caught by the new `maxResults` check on the run that
  added it.
- **The server instructions named only two of the three opt-in tools**,
  omitting `deleteJiraSprint`, and claimed just four tools page at all. Both are
  corrected, and `optInTools` in `cmd/flowtest` now exempts all three.
- The README said "Always on — 23 tools" over a table of 24. It is 28.

## [1.1.1] - 2026-09-27

### Added

- **A section in the server instructions listing the argument names that are not
  what you would guess**: `projectKeyOrId` on the sprint and project tools,
  `issueTypeName` rather than `issueType`, `statusId`/`statusName` rather than a
  transition object, and that `startAt` is a page number and not Atlassian's
  offset. Each of those cost a failed call during the work on strict arguments,
  and an agent should not have to rediscover them. The instructions are the only
  documentation an agent is guaranteed to read.

### Fixed

- The instance paragraph was prepended instead of substituted, so a deployed
  server sent agents a literal `__INSTANCE_CHOICE__` and welded the sentence to
  the next line. The test that should have caught it asserted a substitution the
  test itself performed rather than the one the server does.

## [1.1.0] - 2026-09-27

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
[1.5.0]: https://github.com/rroblf01/jirrabit-mcp/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/rroblf01/jirrabit-mcp/compare/v1.3.0...v1.4.0
[1.2.0]: https://github.com/rroblf01/jirrabit-mcp/compare/v1.1.1...v1.2.0
[1.2.1]: https://github.com/rroblf01/jirrabit-mcp/compare/v1.2.0...v1.2.1
[1.1.1]: https://github.com/rroblf01/jirrabit-mcp/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/rroblf01/jirrabit-mcp/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/rroblf01/jirrabit-mcp/releases/tag/v1.0.0
