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

// CloudID is accepted for Atlassian compatibility and ignored.
type CloudID struct {
	CloudID string `json:"cloudId,omitempty" jsonschema:"Ignored. Accepted because Atlassian's Jira tools require it on every call; jirrabit is single-tenant"`
}

// --- issues ----------------------------------------------------------------

// GetIssueArgs is the input of getJiraIssue.
type GetIssueArgs struct {
	CloudID
	IssueIDOrKey string   `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Fields       []string `json:"fields,omitempty" jsonschema:"Field IDs or names to limit the response to, e.g. summary, status, assignee"`
	Expand       []string `json:"expand,omitempty" jsonschema:"Additional data to inline, e.g. changelog"`
}

// CreateIssueArgs is the input of createJiraIssue.
type CreateIssueArgs struct {
	CloudID
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
	CloudID
	IssueIDOrKey string         `json:"issueIdOrKey" jsonschema:"Issue ID or key to edit"`
	Summary      *string        `json:"summary,omitempty" jsonschema:"New one-line title"`
	Description  *string        `json:"description,omitempty" jsonschema:"New description, in Markdown"`
	Assignee     *string        `json:"assignee,omitempty" jsonschema:"New assignee username. Use an empty string to unassign"`
	StoryPoints  *int           `json:"storyPoints,omitempty" jsonschema:"New story point estimate"`
	DueDate      *string        `json:"dueDate,omitempty" jsonschema:"New due date as YYYY-MM-DD. Use an empty string to clear"`
	Fields       map[string]any `json:"fields,omitempty" jsonschema:"Additional fields as a free-form object"`
	NotifyUsers  *bool          `json:"notifyUsers,omitempty" jsonschema:"Whether to notify watchers. Defaults to jirrabit's own behaviour"`
}

// --- comments --------------------------------------------------------------

// ListCommentsArgs is the input of listJiraIssueComments.
type ListCommentsArgs struct {
	CloudID
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken. Takes precedence over startAt"`
}

// AddOrEditCommentArgs is the input of addOrEditJiraIssueComment. Omitting
// CommentID creates a comment; supplying it replaces that comment's body.
type AddOrEditCommentArgs struct {
	CloudID
	IssueIDOrKey    string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	Body            string `json:"body" jsonschema:"Comment body, in Markdown. When editing, this replaces the whole existing body"`
	CommentID       string `json:"commentId,omitempty" jsonschema:"Numeric id of an existing comment to edit. Omit to create a new comment"`
	VisibilityType  string `json:"visibilityType,omitempty" jsonschema:"Restrict the comment to a group or role"`
	VisibilityValue string `json:"visibilityValue,omitempty" jsonschema:"Group or role name. Required when visibilityType is set"`
}

// --- worklogs --------------------------------------------------------------

// ListWorkLogsArgs is the input of listJiraIssueWorklogs.
type ListWorkLogsArgs struct {
	CloudID
	IssueIDOrKey  string `json:"issueIdOrKey" jsonschema:"Issue ID or key, e.g. WEB-1"`
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// AddOrEditWorkLogArgs is the input of addOrEditJiraIssueWorklog. Omitting
// WorklogID logs new time; supplying it edits that entry.
type AddOrEditWorkLogArgs struct {
	CloudID
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
	CloudID
	StartAt       int    `json:"startAt,omitempty" jsonschema:"1-based page number. Default 1"`
	MaxResults    int    `json:"maxResults,omitempty" jsonschema:"Items per page, 1-200. Default 50"`
	NextPageToken string `json:"nextPageToken,omitempty" jsonschema:"Opaque cursor from a previous response's nextPageToken"`
}

// UpdateProjectArgs is the input of updateJiraProject.
type UpdateProjectArgs struct {
	CloudID
	ProjectKeyOrID string  `json:"projectKeyOrId" jsonschema:"Project key or numeric id, e.g. WEB"`
	Name           *string `json:"name,omitempty" jsonschema:"New project name"`
	Description    *string `json:"description,omitempty" jsonschema:"New project description"`
	Archived       *bool   `json:"archived,omitempty" jsonschema:"Whether the project is archived"`
}
