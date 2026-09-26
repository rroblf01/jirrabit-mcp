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
type GetIssueArgs struct {
	Target
	IssueIDOrKey string   `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Fields       []string `json:"fields,omitempty" jsonschema:"Field IDs or names to limit the response to, e.g. summary, status, assignee"`
	Expand       []string `json:"expand,omitempty" jsonschema:"Additional data to inline, e.g. changelog"`
}

// CreateIssueArgs is the input of createJiraIssue.
type CreateIssueArgs struct {
	Target
	ProjectKey    string         `json:"projectKey" jsonschema:"Key of the project to create the issue in, e.g. WEB"`
	Summary       string         `json:"summary" jsonschema:"One-line issue title"`
	Description   string         `json:"description,omitempty" jsonschema:"Longer description, in Markdown"`
	IssueTypeName string         `json:"issueTypeName,omitempty" jsonschema:"Issue type by name, e.g. Task, Bug, Story"`
	IssueTypeID   *int           `json:"issueTypeId,omitempty" jsonschema:"Issue type by ID; takes precedence over issueTypeName"`
	Assignee      string         `json:"assignee,omitempty" jsonschema:"Username to assign the issue to. Must belong to the project"`
	DueDate       string         `json:"dueDate,omitempty" jsonschema:"Due date as YYYY-MM-DD"`
	StoryPoints   *int           `json:"storyPoints,omitempty" jsonschema:"Story point estimate"`
	Fields        map[string]any `json:"fields,omitempty" jsonschema:"Additional fields as a free-form object, e.g. {\"labels\": [\"backend\"]}"`
}

// EditIssueArgs is the input of editJiraIssue.
type EditIssueArgs struct {
	Target
	IssueIDOrKey string         `json:"issueIdOrKey" jsonschema:"Issue ID or key to edit"`
	Summary      *string        `json:"summary,omitempty" jsonschema:"New one-line title"`
	Description  *string        `json:"description,omitempty" jsonschema:"New description, in Markdown"`
	Assignee     *string        `json:"assignee,omitempty" jsonschema:"New assignee username. Use an empty string to unassign"`
	StoryPoints  *int           `json:"storyPoints,omitempty" jsonschema:"New story point estimate"`
	DueDate      *string        `json:"dueDate,omitempty" jsonschema:"New due date as YYYY-MM-DD. Use an empty string to clear"`
	Fields       map[string]any `json:"fields,omitempty" jsonschema:"Additional fields as a free-form object, for ids with no named argument: statusId, priorityId, sprintId. A status change here is validated against the issue's workflow, and a disallowed one is rejected. An unrecognised key is an error rather than ignored"`
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
	VisibilityType  string `json:"visibilityType,omitempty" jsonschema:"Restrict the comment to a group or role"`
	VisibilityValue string `json:"visibilityValue,omitempty" jsonschema:"Group or role name. Required when visibilityType is set"`
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
	Started          string `json:"started,omitempty" jsonschema:"ISO-8601 timestamp. Defaults to now"`
	WorklogID        string `json:"worklogId,omitempty" jsonschema:"Numeric id of an existing worklog to edit. Omit to log new time"`
	NewEstimate      string `json:"newEstimate,omitempty" jsonschema:"Set the issue's remaining estimate after logging, e.g. '3h'"`
	AdjustEstimate   string `json:"adjustEstimate,omitempty" jsonschema:"'auto' to change the remaining estimate by the logged amount, or 'leave' to leave it untouched"`
	ReduceBy         string `json:"reduceBy,omitempty" jsonschema:"Amount to subtract from the remaining estimate, e.g. '30m'"`
}

// --- projects and users ----------------------------------------------------

// ListProjectsArgs is the input of listJiraProjects.
type ListProjectsArgs struct {
	Target
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
type SprintArgs struct {
	Target
	ProjectKeyOrID string `json:"projectKeyOrId,omitempty" jsonschema:"Project key the sprints belong to. Required for listJiraSprints, ignored by getJiraSprint"`
	SprintID       int    `json:"sprintId,omitempty" jsonschema:"Numeric sprint id, for getJiraSprint"`
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
