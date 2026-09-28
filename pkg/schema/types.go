package schema

// Typed argument structs for tools whose input is nested or typed. Tools with a
// couple of flat string arguments read them with request.GetString instead and
// skip the struct entirely.
//
// Note on the `jsonschema` tag: mcp-go's WithInputSchema uses
// github.com/google/jsonschema-go, whose "jsonschema" struct tag is just a
// plain description string (no "required"/"default=" mini-language) — a field
// is required simply by NOT having `omitempty` in its `json` tag.
//
// Every tool also carries CloudID. Atlassian's MCP tools require a cloudId on
// every call, so agents pass one out of habit. jirrabit is single-tenant, so
// the field is accepted and ignored rather than rejected: making it mandatory
// would break every agent trained against the real thing for no benefit.

// Target names which jirrabit instance a call is about.
//
// This occupies the slot Atlassian's cloudId fills, and is what lets one
// deployed jirrabit-mcp serve many instances. InstanceURL and APIKey are
// optional: omit both and the server uses its own configured default, which is
// the convenient case for a single-user deployment. Supplying a key without a
// URL is rejected rather than paired with the default instance.
//
// CloudID is accepted and ignored purely so agents trained on the real Atlassian
// tools do not break on an unexpected extra requirement.
type Target struct {
	InstanceURL string `json:"instanceUrl,omitempty" jsonschema:"Base URL of the jirrabit instance, e.g. https://jirrabit.example.com. Omit to use this server's default instance"`
	APIKey      string `json:"apiKey,omitempty" jsonschema:"API key for that instance, from jirrabit's API keys page. Omit to use this server's default key"`
	CloudID     string `json:"cloudId,omitempty" jsonschema:"Ignored. Accepted because Atlassian's Jira tools require it on every call"`
}

// --- issues ----------------------------------------------------------------

// GetIssueArgs is the input of getJiraIssue.
//
// Atlassian's tool also takes `fields` and `expand`, to narrow the response and
// to inline a changelog. jirrabit has no equivalent on GET
// /api/v1/issues/{key}/, so they are not declared: an argument this server
// accepts and ignores is worse than one it refuses, because the caller cannot
// tell a narrowed response from a full one. `expand=changelog` in particular
// would have to return a changelog jirrabit does not serve, and the only
// honest answer is a rejection. Both are listed in the server instructions
// under what is not available here.
type GetIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
}

// CreateIssueArgs is the input of createJiraIssue.
type CreateIssueArgs struct {
	Target
	ProjectKey    string `json:"projectKey" jsonschema:"Key of the project to create the issue in, e.g. WEB"`
	Summary       string `json:"summary" jsonschema:"One-line issue title"`
	Description   string `json:"description,omitempty" jsonschema:"Longer description, in Markdown"`
	IssueTypeName string `json:"issueTypeName,omitempty" jsonschema:"Issue type by name, e.g. Task, Bug, Story"`
	IssueTypeID   *int   `json:"issueTypeId,omitempty" jsonschema:"Issue type by ID; takes precedence over issueTypeName"`
	Assignee      string `json:"assignee,omitempty" jsonschema:"Username to assign the issue to. Must belong to the project"`
	DueDate       string `json:"dueDate,omitempty" jsonschema:"Due date as YYYY-MM-DD"`
	StoryPoints   *int   `json:"storyPoints,omitempty" jsonschema:"Story point estimate"`
	PriorityID    *int   `json:"priorityId,omitempty" jsonschema:"Priority id, from listJiraPriorities. Atlassian's own tool has this field, so an agent trained on it will send it"`
	StatusID      *int   `json:"statusId,omitempty" jsonschema:"Initial status id, from listJiraStatuses. A disallowed transition is rejected by jirrabit's workflow"`
	SprintID      *int   `json:"sprintId,omitempty" jsonschema:"Sprint to add the issue to, from listJiraSprints"`
	// Named rather than left in `fields` because Atlassian's own create tool has
	// them: an agent trained on it will send parent and an estimate without being
	// told this server accepts them, and putting them only in a free-form object
	// is the same "nothing told the agent" problem 1.1.0 fixed for priorityId.
	Parent          string         `json:"parent,omitempty" jsonschema:"Issue key to make this a subtask of, e.g. WEB-1. The parent must be in the same project"`
	EpicID          *int           `json:"epicId,omitempty" jsonschema:"Epic to file this under, from listJiraEpics"`
	EstimateMinutes *int           `json:"estimateMinutes,omitempty" jsonschema:"Estimate in minutes, e.g. 480 for eight hours. This is Jira's originalEstimate"`
	Fields          map[string]any `json:"fields,omitempty" jsonschema:"Additional fields as a free-form object, using jirrabit's own field names, e.g. {\"labels\": [\"backend\"]}"`
}

// EditIssueArgs is the input of editJiraIssue.
type EditIssueArgs struct {
	Target
	IssueIDOrKey string  `json:"issueIdOrKey" jsonschema:"Issue ID or key to edit"`
	Summary      *string `json:"summary,omitempty" jsonschema:"New one-line title"`
	Description  *string `json:"description,omitempty" jsonschema:"New description, in Markdown"`
	Assignee     *string `json:"assignee,omitempty" jsonschema:"New assignee username. Use an empty string to unassign"`
	StoryPoints  *int    `json:"storyPoints,omitempty" jsonschema:"New story point estimate"`
	DueDate      *string `json:"dueDate,omitempty" jsonschema:"New due date as YYYY-MM-DD. Use an empty string to clear"`
	StatusID     *int    `json:"statusId,omitempty" jsonschema:"New status id, from listJiraStatuses. Validated against the issue's workflow, and a disallowed transition is rejected"`
	PriorityID   *int    `json:"priorityId,omitempty" jsonschema:"New priority id, from listJiraPriorities"`
	SprintID     *int    `json:"sprintId,omitempty" jsonschema:"Sprint to move the issue to, from listJiraSprints"`
	EpicID       *int    `json:"epicId,omitempty" jsonschema:"Epic to file this under, from listJiraEpics"`
	// Empty string is meaningful for Parent: it detaches a subtask. A pointer
	// would also do it, but a string that reads as "no parent" is what an agent
	// will actually send, and "" is the only way to say it.
	Parent *string `json:"parent,omitempty" jsonschema:"Issue key to make this a subtask of. An empty string detaches it from its parent"`
	// Labels replaces the whole set, because that is what a PATCH means and
	// because reading it as "add these" would leave no way to remove one.
	Labels          []string       `json:"labels,omitempty" jsonschema:"Replace the issue's labels with this list. Names, not ids; unknown names are created. An empty list removes them all"`
	EstimateMinutes *int           `json:"estimateMinutes,omitempty" jsonschema:"Estimate in minutes, e.g. 480 for eight hours"`
	TimeRemaining   *int           `json:"timeRemainingMinutes,omitempty" jsonschema:"Minutes still estimated as remaining. Logging time decreases it automatically; set this to correct it by hand"`
	Fields          map[string]any `json:"fields,omitempty" jsonschema:"Additional fields as a free-form object, using jirrabit's own field names. An unrecognised key is an error rather than ignored"`
}

// --- comments --------------------------------------------------------------

// ListCommentsArgs is the input of listJiraIssueComments.
type ListCommentsArgs struct {
	Target
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken. Takes precedence over startAt"`
}

// AddOrEditCommentArgs is the input of addOrEditJiraIssueComment. Omitting
// CommentID creates a comment; supplying it replaces that comment's body.
type AddOrEditCommentArgs struct {
	Target
	IssueIDOrKey    string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Body            string `json:"body" jsonschema:"Comment body, in Markdown. When editing, this replaces the whole existing body"`
	CommentID       string `json:"commentId,omitempty" jsonschema:"Numeric id of an existing comment to edit. Omit to create a new comment"`
	VisibilityType  string `json:"visibilityType,omitempty" jsonschema:"Not available here. jirrabit has no groups or roles; sending this is rejected rather than ignored"`
	VisibilityValue string `json:"visibilityValue,omitempty" jsonschema:"Not available here. Only meaningful with visibilityType, which is also unavailable"`
}

// --- worklogs --------------------------------------------------------------

// ListWorkLogsArgs is the input of listJiraIssueWorklogs.
type ListWorkLogsArgs struct {
	Target
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// AddOrEditWorkLogArgs is the input of addOrEditJiraIssueWorklog. Omitting
// WorklogID logs new time; supplying it edits that entry.
type AddOrEditWorkLogArgs struct {
	Target
	IssueIDOrKey     string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	TimeSpentSeconds *int   `json:"timeSpentSeconds,omitempty" jsonschema:"Seconds to log. Required when creating a worklog"`
	TimeSpent        string `json:"timeSpent,omitempty" jsonschema:"Time in Jira duration format, e.g. '2h 30m'. Alternative to timeSpentSeconds"`
	Comment          string `json:"comment,omitempty" jsonschema:"Worklog comment, in Markdown"`
	Started          string `json:"started,omitempty" jsonschema:"When the work happened, as ISO 8601, e.g. 2026-09-20T14:00:00+02:00. Omitted means now. A date without an offset is read in the server's timezone; a future date is refused"`
	WorklogID        string `json:"worklogId,omitempty" jsonschema:"Numeric id of an existing worklog to edit, from listJiraIssueWorklogs. Omit to log new time"`
	NewEstimate      string `json:"newEstimate,omitempty" jsonschema:"Not taken here: estimates live on the issue, so set them with editJiraIssue's estimateMinutes and timeRemainingMinutes. Sending this is rejected rather than ignored"`
	AdjustEstimate   string `json:"adjustEstimate,omitempty" jsonschema:"Not taken here: estimates live on the issue, so set them with editJiraIssue's estimateMinutes and timeRemainingMinutes. Sending this is rejected rather than ignored"`
	ReduceBy         string `json:"reduceBy,omitempty" jsonschema:"Not taken here: estimates live on the issue, so set them with editJiraIssue's estimateMinutes and timeRemainingMinutes. Sending this is rejected rather than ignored"`
}

// CloneIssueArgs is the input of cloneJiraIssue.
type CloneIssueArgs struct {
	Target
	IssueIDOrKey    string `json:"issueIdOrKey" jsonschema:"Issue ID or key to copy, e.g. WEB-1"`
	Summary         string `json:"summary,omitempty" jsonschema:"Summary for the copy. Omitted means the UI's own spelling, '[clon] ' plus the original summary"`
	SprintID        *int   `json:"sprintId,omitempty" jsonschema:"Sprint to place the copy in, from listJiraSprints. This is the 'clone it for next sprint' workflow"`
	IncludeSubtasks bool   `json:"includeSubtasks,omitempty" jsonschema:"Copy direct subtasks as children of the copy, one level only. Off by default: cloning a parent with many children is a bulk create, and the caller should ask for it out loud"`
}

// --- projects and users ----------------------------------------------------

// ListProjectsArgs is the input of listJiraProjects.
type ListProjectsArgs struct {
	Target
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// GetProjectArgs is the input of getJiraProject.
type GetProjectArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
}

// ListProjectIssuesArgs is the input of listJiraProjectIssues.
//
// The two filters are jirrabit's own, and they are exact matches rather than
// JQL's: `status` matches the status name and `assignee` the username. For
// anything looser, searchJiraIssuesUsingJql is the tool that can say "~".
type ListProjectIssuesArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	Status         string `json:"status,omitempty" jsonschema:"Only issues in this status, by exact name, e.g. 'In Progress'"`
	Assignee       string `json:"assignee,omitempty" jsonschema:"Only issues assigned to this username, e.g. bob_dev"`
	StartAt        int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults     int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken  string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// ListUsersArgs is the input of listJiraUsers.
//
// An empty query is the useful case, and it is the reason this tool exists: it
// is how a caller finds a username to assign before it has one.
type ListUsersArgs struct {
	Target
	Query         string `json:"query,omitempty" jsonschema:"Text to match against username, display name, email or name. Omit to list every user"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// UpdateProjectArgs is the input of updateJiraProject.
type UpdateProjectArgs struct {
	Target
	ProjectKeyOrID string  `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	Name           *string `json:"name,omitempty" jsonschema:"New project name"`
	Description    *string `json:"description,omitempty" jsonschema:"New project description"`
	Archived       *bool   `json:"archived,omitempty" jsonschema:"Whether the project is archived"`
}

// --- search ---------------------------------------------------------------

// SearchIssuesArgs is the input of searchJiraIssuesUsingJql.
type SearchIssuesArgs struct {
	Target
	JQL           string `json:"jql" jsonschema:"JQL query. Supported fields: project, key, status, statusCategory, priority, type, label, sprint, epic, assignee, reporter, text; operators = != ~ in and 'is EMPTY' / 'is not EMPTY'; an optional trailing 'ORDER BY field [ASC|DESC]'. A fragment with no operator is a free-text search"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// ListLinkTypesArgs is the input of listJiraIssueLinkTypes.
type ListLinkTypesArgs struct {
	Target
}

// CreateLinkArgs is the input of createJiraIssueLink.
type CreateLinkArgs struct {
	Target
	LinkTypeName    string `json:"linkTypeName" jsonschema:"Link type from listJiraIssueLinkTypes, e.g. 'blocks' or 'relates_to'"`
	InwardIssueKey  string `json:"inwardIssueKey" jsonschema:"The other issue of the link"`
	OutwardIssueKey string `json:"outwardIssueKey" jsonschema:"This issue: the one the link hangs off"`
	Comment         string `json:"comment,omitempty" jsonschema:"Optional note describing the link"`
}

// GetIssueLinksArgs is the input of getJiraIssueLinks.
type GetIssueLinksArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key to read links from, e.g. WEB-1"`
}

// WatchIssueArgs is the input of watchJiraIssue. A link is directional, so
// Watching says which side of it this issue sits on.
type WatchIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	IsWatching   *bool  `json:"isWatching,omitempty" jsonschema:"true to watch, false to unwatch. Defaults to true"`
}

// ListWatchersArgs is the input of listJiraIssueWatchers.
type ListWatchersArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
}

// --- transitions -----------------------------------------------------------

// TransitionIssueArgs is the input of transitionJiraIssue. It exists under
// Atlassian's name because that is the name an agent reaches for when it wants
// to move an issue, and the alternative — editJiraIssue with fields.statusId —
// is discoverable only if the agent already knows that spelling.
type TransitionIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key to move, e.g. WEB-1"`
	StatusID     int    `json:"statusId" jsonschema:"Numeric id of the destination status. Resolve it with listJiraStatuses, or pick a state name like Done and let the server look it up"`
	StatusName   string `json:"statusName,omitempty" jsonschema:"Destination status by name, case-insensitive, e.g. 'In Progress'. An alternative to statusId when the name is easier to come by"`
}

// --- sprints ---------------------------------------------------------------

// SprintArgs is the input of the read-only sprint tools.
// ListSprintsArgs is the input of listJiraSprints. Paginated, like the other
// list tools: a project can hold more sprints than one page, and a tool that
// cannot say "there are more" is worse than one that refuses to page.
type ListSprintsArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key the sprints belong to, e.g. WEB"`
	StartAt        int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults     int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken  string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

type SprintArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId,omitempty" jsonschema:"Project key the sprints belong to. Required for listJiraSprints, ignored by getJiraSprint"`
	SprintID       int    `json:"sprintId,omitempty" jsonschema:"Numeric sprint id, for getJiraSprint"`
}

// DeleteSprintArgs is the input of deleteJiraSprint. It carries the
// confirmation, so it is not SprintArgs: a tool that previews before acting needs
// the token on its own schema, and a shared struct would leave the other sprint
// tools advertising an argument they would reject.
type DeleteSprintArgs struct {
	Target
	SprintID int `json:"sprintId" jsonschema:"Numeric sprint id, from listJiraSprints"`
	Confirmation
}

// DeleteEpicArgs is the input of deleteJiraEpic, for the same reason as
// DeleteSprintArgs.
type DeleteEpicArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id the epic belongs to, e.g. WEB"`
	EpicID         int    `json:"epicId" jsonschema:"Numeric epic id, from listJiraEpics"`
	Confirmation
}

// CreateSprintArgs is the input of createJiraSprint.
type CreateSprintArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKey" jsonschema:"Key of the project the sprint belongs to, e.g. WEB"`
	Name           string `json:"name" jsonschema:"Sprint name, e.g. 'Sprint 14 — Checkout'"`
	Goal           string `json:"goal,omitempty" jsonschema:"What this sprint is for"`
	StartDate      string `json:"startDate,omitempty" jsonschema:"Start date as YYYY-MM-DD"`
	EndDate        string `json:"endDate,omitempty" jsonschema:"End date as YYYY-MM-DD"`
}

// UpdateSprintArgs is the input of updateJiraSprint.
type UpdateSprintArgs struct {
	Target
	SprintID  int     `json:"sprintId" jsonschema:"Numeric id of the sprint to update"`
	Name      *string `json:"name,omitempty" jsonschema:"New sprint name"`
	Goal      *string `json:"goal,omitempty" jsonschema:"New sprint goal"`
	StartDate *string `json:"startDate,omitempty" jsonschema:"New start date, YYYY-MM-DD. Empty string clears it"`
	EndDate   *string `json:"endDate,omitempty" jsonschema:"New end date, YYYY-MM-DD. Empty string clears it"`
}

// --- saved filters and users ------------------------------------------------

// ListSavedFiltersArgs is the input of listJiraSavedFilters.
type ListSavedFiltersArgs struct {
	Target
}

// GetUserArgs is the input of getJiraUser.
type GetUserArgs struct {
	Target
	UserIDOrKey string `json:"userIdOrKey" jsonschema:"Numeric user id or username, e.g. 'bob_dev'"`
}

// --- destructive operations ---
//
// Every one of these embeds Confirm, and the field means the same thing
// throughout: omit it to preview, pass it back to act. See pkg/tools/confirm.go.
//
// They are declared in one block because the grouping is the point. A `confirm`
// argument on one tool and not the next is how a caller learns to omit it.

// Confirmation is embedded by every irreversible tool, so `args.Confirmation.Confirm`
// reads as what it is. It lives here rather than in pkg/tools because the
// argument structs live here, and a tool cannot declare an argument the schema
// does not know about — which is the whole reason the strict check misses a
// field the handler quietly ignores.
type Confirmation struct {
	Confirm string `json:"confirm,omitempty" jsonschema:"Confirmation token from a previous call without it. Omit to preview what would be deleted; pass it back to actually do it"`
}

// DeleteIssueArgs is the input of deleteJiraIssue.
type DeleteIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Confirmation
}

// DeleteProjectArgs is the input of deleteJiraProject.
type DeleteProjectArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	Confirmation
}

// DeleteWorkLogArgs is the input of deleteJiraIssueWorklog.
type DeleteWorkLogArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue the worklog belongs to, e.g. WEB-1"`
	WorklogID    string `json:"worklogId" jsonschema:"Numeric id from listJiraIssueWorklogs"`
	Confirmation
}

// DeleteLinkArgs is the input of deleteJiraIssueLink.
type DeleteLinkArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue the link hangs off, e.g. WEB-1"`
	LinkID       string `json:"linkId" jsonschema:"Numeric id from getJiraIssueLinks"`
	Confirmation
}

// ArchiveIssueArgs is the input of archiveJiraIssue. One call, both directions,
// because archiving and unarchiving are the same field and an agent should not
// have to know which half of it is called "archive".
type ArchiveIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Archived     *bool  `json:"archived,omitempty" jsonschema:"true to archive (default), false to bring it back"`
}

// DeleteCommentArgs is the input of deleteJiraIssueComment. No confirm field:
// jirrabit soft-deletes, so the inverse exists and the tool returns it.
type DeleteCommentArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue the comment belongs to, e.g. WEB-1"`
	CommentID    string `json:"commentId" jsonschema:"Numeric id from listJiraIssueComments"`
}

// RestoreCommentArgs is the input of restoreJiraIssueComment.
type RestoreCommentArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue the comment belongs to, e.g. WEB-1"`
	CommentID    string `json:"commentId" jsonschema:"Numeric id of the deleted comment"`
}

// StartSprintArgs is the input of startJiraSprint.
type StartSprintArgs struct {
	Target
	SprintID int `json:"sprintId" jsonschema:"Numeric sprint id, from listJiraSprints or createJiraSprint"`
}

// CloseSprintArgs is the input of closeJiraSprint.
//
// Carries To because closing is not a state flip: jirrabit moves every
// unfinished issue off the sprint, to CarriesTo when given and back to the
// backlog otherwise. An agent that closes a sprint without saying where the
// unfinished work goes has silently moved it, which is why this is in the
// two-step group rather than treated as a plain edit.
type CloseSprintArgs struct {
	Target
	SprintID  int `json:"sprintId" jsonschema:"Numeric sprint id to close"`
	CarriesTo int `json:"carriesTo,omitempty" jsonschema:"Sprint to move this one's unfinished issues into. Must be in the same project and not closed. Omit to send them back to the backlog"`
	Confirmation
}

// --- planning and membership ---
//
// These exist because jirrabit enforced rules a client could not find out about:
// an assignee had to be a project member, and JQL could filter on an epic. The
// members and epics a client could see were the missing half of both.

// ListEpicsArgs is the input of listJiraEpics.
type ListEpicsArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
}

// CreateEpicArgs is the input of createJiraEpic.
type CreateEpicArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	Name           string `json:"name" jsonschema:"Epic name, e.g. 'Checkout rewrite'"`
	Summary        string `json:"summary,omitempty" jsonschema:"What the epic covers"`
	Color          string `json:"color,omitempty" jsonschema:"Hex colour, e.g. '#1e6fff'"`
}

// EpicArgs is the input of deleteJiraEpic.
type EpicArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	EpicID         int    `json:"epicId" jsonschema:"Numeric epic id, from listJiraEpics"`
}

// CreateSavedFilterArgs is the input of createJiraSavedFilter.
type CreateSavedFilterArgs struct {
	Target
	Name  string `json:"name" jsonschema:"Name to store the search under, e.g. 'my open bugs'"`
	JQL   string `json:"jql" jsonschema:"The query, in the same JQL subset searchJiraIssuesUsingJql takes"`
	Scope string `json:"scope,omitempty" jsonschema:"'private' (default) or 'shared'"`
}

// TransitionsForStatusArgs is the input of listJiraTransitions.
type TransitionsForStatusArgs struct {
	Target
	StatusID int `json:"statusId" jsonschema:"Numeric status id to ask about, from listJiraStatuses"`
}

// --- history, attachments, notifications, teams ----------------------------
//
// Grouped by the question each one answers, because the two history tools are
// routinely confused and the difference is the whole point of having both.

// ChangelogArgs is the input of getJiraIssueChangelog.
type ChangelogArgs struct {
	Target
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Field         string `json:"field,omitempty" jsonschema:"Narrow to one field name, e.g. 'status'"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// ProjectActivityArgs is the input of getJiraProjectActivity.
type ProjectActivityArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	Verb           string `json:"verb,omitempty" jsonschema:"Narrow to one kind of event, e.g. 'created', 'updated' or 'deleted'"`
	StartAt        int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults     int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken  string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// ProjectSlaArgs is the input of getJiraProjectSla.
type ProjectSlaArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	Days           int    `json:"days,omitempty" jsonschema:"Flag issues stuck in one status longer than this many days. Default 7, minimum 1"`
}

// ProjectBurndownArgs is the input of getJiraProjectBurndown.
type ProjectBurndownArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	SprintID       *int   `json:"sprintId,omitempty" jsonschema:"Which sprint to chart, from listJiraSprints. Omitted means the active sprint, else the latest by start date"`
}

// ProjectReportsArgs is the input of getJiraProjectReports.
type ProjectReportsArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
}

// ListAttachmentsArgs is the input of listJiraIssueAttachments.
type ListAttachmentsArgs struct {
	Target
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// GetAttachmentArgs is the input of getJiraAttachment.
type GetAttachmentArgs struct {
	Target
	AttachmentID int `json:"attachmentId" jsonschema:"Numeric attachment id, from listJiraIssueAttachments"`
}

// AddAttachmentArgs is the input of addJiraAttachment.
type AddAttachmentArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Filename     string `json:"filename" jsonschema:"Name to store it under, e.g. 'screenshot.png'"`
	// Data rather than a path: jirrabit keeps attachments base64-encoded in the
	// database, so there is no file on a server to point at and a JSON call
	// cannot carry bytes any other way.
	Data        string `json:"data" jsonschema:"Base64 of the file, with no data: prefix. At most 5 MB of raw bytes, which is about 6.7 MB encoded"`
	ContentType string `json:"contentType,omitempty" jsonschema:"MIME type, e.g. 'image/png'"`
}

// ListNotificationsArgs is the input of listJiraNotifications.
type ListNotificationsArgs struct {
	Target
	UnreadOnly    bool   `json:"unreadOnly,omitempty" jsonschema:"Only the ones not yet read"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// MarkReadArgs is the input of markJiraNotificationsRead. The whole struct is
// the body, and it is always sent — even empty — because jirrabit reads ids from
// the body and a bare POST would be a call with nothing to say.
type MarkReadArgs struct {
	Target
	IDs []int `json:"ids,omitempty" jsonschema:"Notification ids to mark. Omit to mark all of them"`
}

// --- administration ---
//
// Superuser-only, matching the web UI's workflow editor. Two of these are
// pointers where it matters: a value that is absent and a value that is zero are
// different requests, and treating them the same is how a new status ends up at
// the front of the board.

// CreateStatusArgs is the input of createJiraStatus.
type CreateStatusArgs struct {
	Target
	Name     string `json:"name" jsonschema:"Status name, e.g. 'Blocked'"`
	Category string `json:"category,omitempty" jsonschema:"todo, in_progress or done. Affects statusCategory in JQL and whether a change to it resolves the issue. Default todo"`
	// A pointer, and no default: jirrabit orders statuses by this, so a 0 would
	// put the new status at the front and reorder a live board. Omit it to append.
	Order    *int `json:"order,omitempty" jsonschema:"Board order. Omit to put the status at the end; 0 would jump it to the front"`
	WIPLimit *int `json:"wipLimit,omitempty" jsonschema:"Soft limit shown on the board column. Not enforced"`
}

// StatusTransitionsArgs is the input of updateJiraStatusTransitions.
type StatusTransitionsArgs struct {
	Target
	StatusID int `json:"statusId" jsonschema:"Status to configure, from listJiraStatuses"`
	// A pointer, and required. Absent means "do not touch"; an empty list means
	// the workflow is open from this status, which is the opposite of forbidding
	// everything and is the single easiest way to make a strict workflow loose.
	AllowedNext []int `json:"allowedNext" jsonschema:"Replaces the whole list of reachable status ids. An EMPTY LIST MAKES THE WORKFLOW OPEN, so every status becomes reachable — it does not mean 'nowhere'"`
}

// One schema per delete, not one for all three. They are identical apart from
// the argument's name, and a shared schema with a single name would mean
// deleteJiraPriority rejects `priorityId` as unknown while accepting a
// `statusId` that means nothing to it — the caller sending the obvious thing and
// getting told to rename it, for no gain in code.
type DeleteStatusArgs struct {
	Target
	StatusID int `json:"statusId" jsonschema:"Numeric status id, from listJiraStatuses"`
}

type DeletePriorityArgs struct {
	Target
	PriorityID int `json:"priorityId" jsonschema:"Numeric priority id, from listJiraPriorities"`
}

type DeleteIssueTypeArgs struct {
	Target
	IssueTypeID int `json:"issueTypeId" jsonschema:"Numeric issue type id, from listJiraIssueTypeMetadata"`
}

// CreatePriorityArgs is the input of createJiraPriority.
type CreatePriorityArgs struct {
	Target
	Name   string `json:"name" jsonschema:"Priority name, e.g. 'Urgent'"`
	Weight int    `json:"weight,omitempty" jsonschema:"Sort weight; higher sorts first in ORDER BY priority"`
	Color  string `json:"color,omitempty" jsonschema:"Hex colour, e.g. '#dc2626'"`
}

// CreateIssueTypeArgs is the input of createJiraIssueType.
type CreateIssueTypeArgs struct {
	Target
	Name     string `json:"name" jsonschema:"Type name, e.g. 'Chore'"`
	Category string `json:"category,omitempty" jsonschema:"task, bug, story, epic. Default task"`
	Icon     string `json:"icon,omitempty" jsonschema:"Icon name shown on the board"`
	Color    string `json:"color,omitempty" jsonschema:"Hex colour, e.g. '#16a34a'"`
}

// SetCustomFieldValuesArgs is the input of setJiraIssueCustomFieldValues.
type SetCustomFieldValuesArgs struct {
	Target
	IssueIDOrKey string         `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Values       map[string]any `json:"values" jsonschema:"Keyed by the field's slug from listJiraCustomFields. A null removes that key"`
}

// --- board placement ---------------------------------------------------------
//
// An agent could change an issue's status but never put it anywhere on the
// board. `rank` is the card's index inside its column, so these four tools are
// how an agent stops describing a board and starts driving one.

// MoveIssueArgs is the input of moveJiraIssue.
type MoveIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	TargetStatus string `json:"targetStatus,omitempty" jsonschema:"Status to move the card to, e.g. 'In Progress'. Omit to keep the current status and only change position"`
	Position     *int   `json:"position,omitempty" jsonschema:"0-based index in the destination column. Omit to put the card at the end, which is the only honest thing to do when no position was given"`
}

// ReorderBoardColumnArgs is the input of reorderJiraBoardColumn.
type ReorderBoardColumnArgs struct {
	Target
	ProjectKeyOrID string   `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	TargetStatus   string   `json:"targetStatus" jsonschema:"Status whose column to renumber, e.g. 'To Do'"`
	IssueKeys      []string `json:"issueKeys" jsonschema:"The whole column, as the board's drag sends it. Two cards swapping places is then the same call as a column of twenty"`
}

// BulkUpdateBoardArgs is the input of bulkUpdateJiraBoard.
type BulkUpdateBoardArgs struct {
	Target
	ProjectKeyOrID string   `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	IssueKeys      []string `json:"issueKeys" jsonschema:"Issue keys to act on. A key from another project is rejected"`
	Action         string   `json:"action" jsonschema:"One of status, assignee, priority, sprint, epic, label_add, label_remove"`
	Value          string   `json:"value,omitempty" jsonschema:"Depends on the action: a status name for status, a username for assignee, a priority name, a sprint name or id, an epic id, a label name for label_add/label_remove"`
}

// --- personal state ----------------------------------------------------------
//
// Pins, timers, snoozes, reactions, branch links and issue templates. Every one
// of these existed in jirrabit's UI with no HTTP path, so an agent could not
// read or change any of it.

// ListPinsArgs is the input of listJiraPins.
type ListPinsArgs struct {
	Target
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// PinIssueArgs is the input of pinJiraItem.
type PinIssueArgs struct {
	Target
	IssueIDOrKey   string `json:"issueIdOrKey,omitempty" jsonschema:"Issue ID or key to pin, e.g. WEB-1. Give exactly one of this or projectKeyOrId"`
	ProjectKeyOrID string `json:"projectKeyOrId,omitempty" jsonschema:"Project key or id to pin, e.g. WEB. Give exactly one of this or issueIdOrKey"`
}

// UnpinItemArgs is the input of unpinJiraItem.
type UnpinItemArgs struct {
	Target
	PinID int `json:"pinId" jsonschema:"Pin id from listJiraPins"`
}

// StartIssueTimerArgs is the input of startJiraIssueTimer.
type StartIssueTimerArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
}

// GetIssueTimerArgs is the input of getJiraIssueTimer.
type GetIssueTimerArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
}

// StopIssueTimerArgs is the input of stopJiraIssueTimer.
type StopIssueTimerArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
}

// SnoozeIssueArgs is the input of snoozeJiraIssueNotifications.
type SnoozeIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Until        string `json:"until" jsonschema:"ISO-8601 instant to mute until, e.g. 2026-01-08T09:00:00Z. Must be in the future"`
}

// UnsnoozeIssueArgs is the input of unsnoozeJiraIssueNotifications.
type UnsnoozeIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
}

// ListCommentReactionsArgs is the input of listJiraCommentReactions.
type ListCommentReactionsArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	CommentID    int    `json:"commentId" jsonschema:"Numeric comment id, from the issue's comments"`
}

// ReactToCommentArgs is the input of reactToJiraComment.
type ReactToCommentArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	CommentID    int    `json:"commentId" jsonschema:"Numeric comment id"`
	Emoji        string `json:"emoji" jsonschema:"One of the supported reactions: tada, rocket, eyes, heart, fire, thinking"`
}

// RemoveCommentReactionArgs is the input of removeJiraCommentReaction.
type RemoveCommentReactionArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	CommentID    int    `json:"commentId" jsonschema:"Numeric comment id"`
	Emoji        string `json:"emoji" jsonschema:"The reaction to remove"`
}

// ListBranchLinksArgs is the input of listJiraIssueBranchLinks.
type ListBranchLinksArgs struct {
	Target
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// LinkBranchToIssueArgs is the input of linkJiraBranchToIssue.
type LinkBranchToIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Branch       string `json:"branch" jsonschema:"Branch name, e.g. feat/login"`
	RepoURL      string `json:"repoUrl,omitempty" jsonschema:"Repository URL. Must be a valid URL if given"`
	CommitSHA    string `json:"commitSha,omitempty" jsonschema:"Commit hash this link points at"`
	Message      string `json:"message,omitempty" jsonschema:"Short description of the branch's work"`
}

// UnlinkBranchFromIssueArgs is the input of unlinkJiraBranchFromIssue.
type UnlinkBranchFromIssueArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	LinkID       int    `json:"linkId" jsonschema:"Branch link id from listJiraIssueBranchLinks"`
}

// ListIssueTemplatesArgs is the input of listJiraIssueTemplates.
type ListIssueTemplatesArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	StartAt        int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults     int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken  string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// CreateIssueTemplateArgs is the input of createJiraIssueTemplate.
type CreateIssueTemplateArgs struct {
	Target
	ProjectKeyOrID string   `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Name           string   `json:"name" jsonschema:"Template name, e.g. 'Bug report'. Unique within the project"`
	IssueTypeID    int      `json:"issueTypeId" jsonschema:"Numeric issue type id, from listJiraIssueTypeMetadata"`
	Summary        string   `json:"summary,omitempty" jsonschema:"Default summary to prefill"`
	Description    string   `json:"description,omitempty" jsonschema:"Default description, in markdown"`
	PriorityID     *int     `json:"priorityId,omitempty" jsonschema:"Numeric priority id, from listJiraPriorityMetadata"`
	Labels         []string `json:"labels,omitempty" jsonschema:"Label names to apply on creation; created if missing"`
}

// UpdateIssueTemplateArgs is the input of updateJiraIssueTemplate.
type UpdateIssueTemplateArgs struct {
	Target
	ProjectKeyOrID string   `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	TemplateID     int      `json:"templateId" jsonschema:"Template id from listJiraIssueTemplates"`
	Name           *string  `json:"name,omitempty" jsonschema:"New template name. Unique within the project"`
	IssueTypeID    *int     `json:"issueTypeId,omitempty" jsonschema:"Numeric issue type id, from listJiraIssueTypeMetadata"`
	Summary        *string  `json:"summary,omitempty" jsonschema:"New default summary to prefill"`
	Description    *string  `json:"description,omitempty" jsonschema:"New default description, in markdown"`
	PriorityID     *int     `json:"priorityId,omitempty" jsonschema:"Numeric priority id, from listJiraPriorityMetadata. Cannot clear on its own: a null and an absent value arrive the same way, so use clearPriority to remove the default priority"`
	ClearPriority  bool     `json:"clearPriority,omitempty" jsonschema:"true removes the template's default priority"`
	Labels         []string `json:"labels,omitempty" jsonschema:"Label names, replacing the whole set; created if missing. Read the template first or the labels it had are gone. An empty list clears them"`
}

// DeleteIssueTemplateArgs is the input of deleteJiraIssueTemplate.
type DeleteIssueTemplateArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	TemplateID     int    `json:"templateId" jsonschema:"Template id from listJiraIssueTemplates"`
	Confirmation
}

// --- project and membership --------------------------------------------------
//
// createJiraProject and the membership writes had endpoints and no tools, so a
// client could be handed a project and could not put a second person in it.

// CreateProjectArgs is the input of createJiraProject.
type CreateProjectArgs struct {
	Target
	Key         string            `json:"key" jsonschema:"Short uppercase project key, 1-10 characters, e.g. WEB. It is the prefix of every issue key in the project"`
	Name        string            `json:"name" jsonschema:"Human name, e.g. 'Website'"`
	Description string            `json:"description,omitempty" jsonschema:"One line about what the project is for"`
	Lead        string            `json:"lead,omitempty" jsonschema:"Username of the project lead. The creator is the lead unless this names somebody else"`
	Members     []CreateMemberArg `json:"members,omitempty" jsonschema:"People to add with a role. A bad username fails the whole creation rather than leaving a half-built project"`
}

// CreateMemberArg is one entry of CreateProjectArgs.Members.
type CreateMemberArg struct {
	Username string `json:"username" jsonschema:"Username of the person to add"`
	Role     string `json:"role,omitempty" jsonschema:"admin, member or viewer. Default member"`
}

// AddProjectMemberArgs is the input of addJiraProjectMember.
type AddProjectMemberArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Username       string `json:"username" jsonschema:"Username of the person to add"`
	Role           string `json:"role,omitempty" jsonschema:"admin, member or viewer. Default member"`
}

// UpdateProjectMemberArgs is the input of updateJiraProjectMember.
type UpdateProjectMemberArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Username       string `json:"username" jsonschema:"Username of the member to change"`
	Role           string `json:"role" jsonschema:"The new role: admin, member or viewer"`
}

// RemoveProjectMemberArgs is the input of removeJiraProjectMember.
type RemoveProjectMemberArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Username       string `json:"username" jsonschema:"Username of the member to remove"`
	Confirmation
}

// UpdateEpicArgs is the input of updateJiraEpic.
type UpdateEpicArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	EpicID         int    `json:"epicId" jsonschema:"Numeric epic id, from listJiraEpics"`
	Name           string `json:"name,omitempty" jsonschema:"New name"`
	Summary        string `json:"summary,omitempty" jsonschema:"New summary"`
	Color          string `json:"color,omitempty" jsonschema:"New hex colour, e.g. '#dc2626'"`
	Done           *bool  `json:"done,omitempty" jsonschema:"true to close the epic, false to reopen it"`
}

// DeleteSavedFilterArgs is the input of deleteJiraSavedFilter.
type DeleteSavedFilterArgs struct {
	Target
	FilterID int `json:"filterId" jsonschema:"Numeric filter id, from listJiraSavedFilters"`
	Confirmation
}

// DeleteAttachmentArgs is the input of deleteJiraAttachment.
type DeleteAttachmentArgs struct {
	Target
	AttachmentID int `json:"attachmentId" jsonschema:"Numeric attachment id, from listJiraIssueAttachments"`
	Confirmation
}

// GetCommentHistoryArgs is the input of getJiraCommentHistory.
type GetCommentHistoryArgs struct {
	Target
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	CommentID     int    `json:"commentId" jsonschema:"Numeric comment id"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// --- vocabulary updates -----------------------------------------------------
//
// The creates arrived in an earlier release and the updates did not, so a status
// could be renamed only by the web UI. That is a strange gap: the create tool
// needed a way to fix a typo in what it had just made.

// IDOnlyArgs is the input of the tools that take one numeric id and nothing else.
type IDOnlyArgs struct {
	Target
	ID int `json:"id" jsonschema:"Numeric id, from the matching listJira*Metadata tool"`
}

// UpdateStatusArgs is the input of updateJiraStatus.
type UpdateStatusArgs struct {
	Target
	StatusID int    `json:"statusId" jsonschema:"Numeric status id, from listJiraStatuses"`
	Name     string `json:"name,omitempty" jsonschema:"New name, up to 40 characters"`
	Category string `json:"category,omitempty" jsonschema:"todo, in_progress or done. The category is what decides whether an issue in this status counts as resolved"`
	Order    *int   `json:"order,omitempty" jsonschema:"Board column order. Lower sorts first. A status with no order is not reordered, which is the difference between renaming and reordering the board"`
	WipLimit *int   `json:"wipLimit,omitempty" jsonschema:"Work-in-progress limit for the column"`
}

// UpdatePriorityArgs is the input of updateJiraPriority.
type UpdatePriorityArgs struct {
	Target
	PriorityID int    `json:"priorityId" jsonschema:"Numeric priority id, from listJiraPriorities"`
	Name       string `json:"name,omitempty" jsonschema:"New name, up to 20 characters — the column is shorter than a status name's"`
	Weight     *int   `json:"weight,omitempty" jsonschema:"Sort weight; higher sorts first in ORDER BY priority"`
	Color      string `json:"color,omitempty" jsonschema:"New hex colour, e.g. '#dc2626'"`
}

// UpdateIssueTypeArgs is the input of updateJiraIssueType.
type UpdateIssueTypeArgs struct {
	Target
	IssueTypeID int    `json:"issueTypeId" jsonschema:"Numeric issue type id, from listJiraIssueTypeMetadata"`
	Name        string `json:"name,omitempty" jsonschema:"New name, up to 40 characters"`
	Category    string `json:"category,omitempty" jsonschema:"task, bug, story or epic"`
	Icon        string `json:"icon,omitempty" jsonschema:"Icon name shown on the board, up to 8 characters"`
	Color       string `json:"color,omitempty" jsonschema:"New hex colour, e.g. '#16a34a'"`
}

// UpdateLabelArgs is the input of updateJiraLabel.
type UpdateLabelArgs struct {
	Target
	LabelID int    `json:"labelId" jsonschema:"Numeric label id, from listJiraLabels"`
	Name    string `json:"name,omitempty" jsonschema:"New name, up to 40 characters"`
	Color   string `json:"color,omitempty" jsonschema:"New hex colour, e.g. '#dc2626'"`
}

// DeleteLabelArgs is the input of deleteJiraLabel.
type DeleteLabelArgs struct {
	Target
	LabelID int `json:"labelId" jsonschema:"Numeric label id, from listJiraLabels"`
	Confirmation
}

// CreateCustomFieldArgs is the input of createJiraCustomField.
type CreateCustomFieldArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Name           string `json:"name" jsonschema:"Field name as it appears on the issue form, e.g. 'Story points'"`
	Slug           string `json:"slug,omitempty" jsonschema:"Key the value is stored under. Derived from the name when omitted, and unique per project"`
	Type           string `json:"type,omitempty" jsonschema:"text, textarea, number, select, date, user or checkbox. Default text"`
	Required       bool   `json:"required,omitempty" jsonschema:"Whether an issue must set this field"`
	Options        string `json:"options,omitempty" jsonschema:"Comma-separated choices, for type=select"`
	Order          int    `json:"order,omitempty" jsonschema:"Display order among the project's fields"`
}

// DeleteCustomFieldArgs is the input of deleteJiraCustomField.
type DeleteCustomFieldArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	FieldID        int    `json:"fieldId" jsonschema:"Numeric custom field id, from listJiraCustomFields"`
	Confirmation
}

// WebhookArgs is the input of createJiraWebhook, updateJiraWebhook and
// deleteJiraWebhook. One struct for all three, because they are the same row and
// the same fields; a delete that took fewer arguments would be a second
// declaration of the same thing to keep in step.
type WebhookArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id the hook belongs to, e.g. WEB"`
	WebhookID      int    `json:"webhookId,omitempty" jsonschema:"Numeric webhook id, from listJiraWebhooks. Required for update and delete"`
	Name           string `json:"name,omitempty" jsonschema:"Hook name, up to 80 characters"`
	Action         string `json:"action,omitempty" jsonschema:"What the hook does, up to 120 characters"`
	Entity         string `json:"entity,omitempty" jsonschema:"Entity the hook watches. Default issue"`
	Event          string `json:"event,omitempty" jsonschema:"Event to fire on, e.g. issue.updated. Default issue.updated"`
	StateFilter    string `json:"stateFilter,omitempty" jsonschema:"Comma-separated status names; empty fires on any state"`
	Active         *bool  `json:"active,omitempty" jsonschema:"Whether the hook fires. Turning one off is how you stop it without losing it"`
	Confirmation
}

// UpdateProjectWikiArgs is the input of updateJiraProjectWiki.
type UpdateProjectWikiArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Body           string `json:"body" jsonschema:"The whole wiki page, replacing whatever was there, in markdown"`
}

// TeamArgs is the input of createJiraTeam and updateJiraTeam.
type TeamArgs struct {
	Target
	TeamID      int      `json:"teamId,omitempty" jsonschema:"Numeric team id, from listJiraTeams. Required for update"`
	Slug        string   `json:"slug" jsonschema:"Short handle, e.g. 'platform'. Unique across the instance"`
	Name        string   `json:"name" jsonschema:"Team name, e.g. 'Platform'"`
	Description string   `json:"description,omitempty" jsonschema:"One line about what the team does"`
	Members     []string `json:"members,omitempty" jsonschema:"Usernames. The whole list is replaced, not merged, so read it first with listJiraTeams"`
}

// DeleteTeamArgs is the input of deleteJiraTeam.
type DeleteTeamArgs struct {
	Target
	TeamID int `json:"teamId" jsonschema:"Numeric team id, from listJiraTeams"`
	Confirmation
}

// --- API keys and users ------------------------------------------------------

// ListAPIKeysArgs is the input of listJiraApiKeys.
type ListAPIKeysArgs struct {
	Target
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// CreateAPIKeyArgs is the input of createJiraApiKey.
type CreateAPIKeyArgs struct {
	Target
	Name string `json:"name" jsonschema:"Label for the key, so it can be told apart later, e.g. 'ci'"`
}

// DeleteAPIKeyArgs is the input of deleteJiraApiKey.
type DeleteAPIKeyArgs struct {
	Target
	KeyID int `json:"keyId" jsonschema:"Numeric key id, from listJiraApiKeys"`
	Confirmation
}

// ListAdminUsersArgs is the input of listJiraAdminUsers.
type ListAdminUsersArgs struct {
	Target
	Query         string `json:"query,omitempty" jsonschema:"Filter by username or email"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// CreateAdminUserArgs is the input of createJiraAdminUser.
type CreateAdminUserArgs struct {
	Target
	Username    string `json:"username" jsonschema:"Login name, unique across the instance"`
	Email       string `json:"email,omitempty" jsonschema:"Email address"`
	DisplayName string `json:"displayName,omitempty" jsonschema:"Name to show instead of the username"`
	Password    string `json:"password,omitempty" jsonschema:"Initial password. Omit and jirrabit makes an unusable one, so the person has to reset it"`
	IsSuperuser bool   `json:"isSuperuser,omitempty" jsonschema:"Grant instance-wide admin. This is the flag that bypasses every project permission check"`
}

// UpdateAdminUserArgs is the input of updateJiraAdminUser.
type UpdateAdminUserArgs struct {
	Target
	UserID      int     `json:"userId" jsonschema:"Numeric user id, from listJiraAdminUsers"`
	Email       *string `json:"email,omitempty" jsonschema:"New email address"`
	DisplayName *string `json:"displayName,omitempty" jsonschema:"New display name"`
	IsSuperuser *bool   `json:"isSuperuser,omitempty" jsonschema:"true to grant instance-wide admin, false to take it away"`
	IsActive    *bool   `json:"isActive,omitempty" jsonschema:"false deactivates the account: the person cannot log in and existing sessions stop working"`
}

// UpdateCurrentUserArgs is the input of updateJiraCurrentUser.
type UpdateCurrentUserArgs struct {
	Target
	DisplayName *string  `json:"displayName,omitempty" jsonschema:"Name shown instead of the username"`
	FirstName   *string  `json:"firstName,omitempty" jsonschema:"Given name"`
	LastName    *string  `json:"lastName,omitempty" jsonschema:"Family name"`
	Email       *string  `json:"email,omitempty" jsonschema:"Email address. Empty clears it, which also stops notification emails since there is nowhere to send them"`
	JobTitle    *string  `json:"jobTitle,omitempty" jsonschema:"Job title shown on the profile"`
	Timezone    *string  `json:"timezone,omitempty" jsonschema:"IANA timezone, e.g. Europe/Madrid. jirrabit rejects unknown zones rather than guessing"`
	Language    *string  `json:"language,omitempty" jsonschema:"Interface language: es or en"`
	Palette     *string  `json:"palette,omitempty" jsonschema:"Colour palette: blue, ocean, forest, violet, sunset, rose, midnight or contrast"`
	NotifyEmail *bool    `json:"notifyEmail,omitempty" jsonschema:"false stops notification emails; in-app notices still arrive"`
	MutedKinds  []string `json:"mutedKinds,omitempty" jsonschema:"Notification kinds to mute: assigned, mention, comment, status, watch. The whole list is replaced, not merged, so read the current profile first"`
	Avatar      *string  `json:"avatar,omitempty" jsonschema:"Avatar as a data URL, data:image/png;base64,.... Empty string clears the avatar. PNG, JPEG, GIF or WebP, at most ~1.1 MB decoded"`
}

// --- invites -----------------------------------------------------------------

// ListInvitesArgs is the input of listJiraInvites.
type ListInvitesArgs struct {
	Target
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// CreateInviteArgs is the input of createJiraInvite.
type CreateInviteArgs struct {
	Target
	Email string `json:"email,omitempty" jsonschema:"Optional email hint. It is not enforced and is not sent anywhere"`
	Role  string `json:"role,omitempty" jsonschema:"member, admin or viewer. Default member"`
	Days  int    `json:"days,omitempty" jsonschema:"How long the invite stays valid, 1-365 days. Default 7"`
}

// RevokeInviteArgs is the input of revokeJiraInvite.
type RevokeInviteArgs struct {
	Target
	InviteID int `json:"inviteId" jsonschema:"Numeric invite id, from listJiraInvites"`
}

// --- CSV ---------------------------------------------------------------------
//
// Import is the only bulk write in the API that an agent can reach, and it is
// two-phase on purpose: a preview, then an apply. A CSV of two hundred rows is
// not something to create on a first reading of the file.

// ExportIssuesCSVArgs is the input of exportJiraIssuesCsv.
type ExportIssuesCSVArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Columns        string `json:"columns,omitempty" jsonschema:"Comma-separated subset of: key, summary, status, priority, type, assignee, reporter, sprint, epic, story_points, estimate_minutes, time_spent_minutes, due_date, resolved_at, created_at, updated_at. Omit for all"`
	Text           string `json:"text,omitempty" jsonschema:"Only issues whose key or summary contains this"`
	Status         int    `json:"status,omitempty" jsonschema:"Only issues in this status id"`
	Assignee       string `json:"assignee,omitempty" jsonschema:"'me' for yourself, or a numeric user id"`
	Archived       bool   `json:"archived,omitempty" jsonschema:"Include archived issues, which are off the board by default"`
}

// ImportIssuesCSVArgs is the input of importJiraIssuesCsv.
type ImportIssuesCSVArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id to create the issues in, e.g. WEB"`
	CSV            string `json:"csv" jsonschema:"The CSV text. Columns: summary (required), description, type, priority, assignee, story_points, due_date. Unknown columns are ignored"`
	DryRun         *bool  `json:"dryRun,omitempty" jsonschema:"true to preview without creating anything. Defaults to true, which is the only way to see what a malformed CSV would do"`
	Confirmation
}

// --- board views, recent and mentions ---------------------------------------

// ListBoardViewsArgs is the input of listJiraBoardViews.
type ListBoardViewsArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	StartAt        int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults     int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken  string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// SaveBoardViewArgs is the input of saveJiraBoardView.
type SaveBoardViewArgs struct {
	Target
	ProjectKeyOrID string            `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	Name           string            `json:"name" jsonschema:"Name for the view, unique for you in this project"`
	Filters        map[string]string `json:"filters,omitempty" jsonschema:"Board filter values: assignee, type, priority, epic, sprint, stale, due, text. An unrecognised key is refused rather than stored, because the board would never read it"`
	IsDefault      bool              `json:"isDefault,omitempty" jsonschema:"Auto-apply when you open this project's board. Only one view per project can be the default"`
}

// DeleteBoardViewArgs is the input of deleteJiraBoardView.
type DeleteBoardViewArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId" jsonschema:"Project key or id, e.g. WEB"`
	ViewID         int    `json:"viewId" jsonschema:"Numeric board view id, from listJiraBoardViews"`
}

// ListRecentIssuesArgs is the input of listJiraRecentIssues.
type ListRecentIssuesArgs struct {
	Target
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// ListCommentMentionsArgs is the input of listJiraCommentMentions.
type ListCommentMentionsArgs struct {
	Target
	IssueIDOrKey string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	CommentID    int    `json:"commentId" jsonschema:"Numeric comment id"`
}
