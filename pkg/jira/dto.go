package jira

// The types in this file mirror jirrabit's REST API schemas (jirrabit/api.py)
// exactly. They are the wire format only — tools return the Jira-shaped values
// from shapes.go, never these.
//
// Fields that jirrabit does not expose yet are declared as optional and
// omitted when absent, so the Jira payloads degrade gracefully instead of
// inventing data. Each such field lists the endpoint change that will fill it.

// User mirrors accounts' `UserOut`.
type User struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// Project mirrors `ProjectOut`.
type Project struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
}

// Sprint mirrors `SprintOut`.
type Sprint struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Goal       string  `json:"goal"`
	Status     string  `json:"status"`
	StartDate  *string `json:"start_date"`
	EndDate    *string `json:"end_date"`
	RetroNotes string  `json:"retro_notes"`
}

// Issue mirrors `IssueOut`.
//
// The fields below `Project` were written before jirrabit's `IssueOut` carried
// them. Every one of them is now emitted, so none of them is optional any more
// and a zero here means jirrabit really sent a zero.
//
// The tags have to match `IssueOut` exactly, and they are not the names the
// shapes in `shapes.go` want. `TypeID` in particular is the one field where the
// two disagree: jirrabit calls it `issue_type_id` and nothing calls it `type_id`,
// so a tag that guessed the shape's name instead of the API's left
// `fields.issueType.id` permanently empty while every other field arrived.
// A response is not proof of a tag, which is why there is a test for this.
//
// `Components` was the opposite mistake: it is declared here but jirrabit has no
// `components` field on an issue at all, so it is omitted from the payload
// rather than shipped as a confident empty list.
type Issue struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	// Status is the status *name* as a bare string in the current API. When the
	// enriched fields are available, shapes.go still emits a Jira status object
	// and uses this only as the object's name.
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Type     string `json:"type"`
	Project  string `json:"project"`
	// Assignee and Reporter are usernames, or null. Jira expects user objects
	// with an accountId and displayName; shapes.go builds them from the username
	// until jirrabit exposes account ids.
	Assignee *string `json:"assignee"`
	Reporter *string `json:"reporter"`
	// StoryPoints is Jira's "customfield_10016".
	StoryPoints      *int    `json:"story_points"`
	DueDate          *string `json:"due_date"`
	EstimateMinutes  *int    `json:"estimate_minutes"`
	TimeSpentMinutes int     `json:"time_spent_minutes"`

	// These are all populated by IssueOut. A zero means jirrabit sent a zero.
	Created        string   `json:"created"`
	Updated        string   `json:"updated"`
	StatusID       int      `json:"status_id"`
	StatusCategory string   `json:"status_category"`
	PriorityID     int      `json:"priority_id"`
	TypeID         int      `json:"issue_type_id"`
	Labels         []string `json:"labels"`
	Parent         string   `json:"parent"`
	SprintID       *int     `json:"sprint_id"`
	Archived       bool     `json:"archived"`
	// Epic was readable and unwritable in jirrabit for a long time, so it is
	// carried here for the first time. Rendered as a named object rather than a
	// bare string, to match priority and issuetype beside it.
	Epic   string `json:"epic"`
	EpicID int    `json:"epic_id"`
	// TimeRemainingMinutes has no direct Jira field; it is the counterpart of
	// timeoriginalestimate and is reported so an agent can see the gap.
	TimeRemainingMinutes *int `json:"time_remaining_minutes"`
	// CommentCount and WorklogCount are read hints, not data: a zero means
	// "nothing worth a second call", so a client deciding whether to list
	// comments or worklogs does not spend a call to learn the list is empty.
	CommentCount int `json:"comment_count"`
	WorklogCount int `json:"worklog_count"`
}

// Comment mirrors `CommentOut`.
type Comment struct {
	ID        int    `json:"id"`
	Issue     string `json:"issue"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	Edited    bool   `json:"edited"`
}

// WorkLog mirrors `WorkLogOut`.
type WorkLog struct {
	ID       int    `json:"id"`
	Issue    string `json:"issue"`
	Author   string `json:"author"`
	Minutes  int    `json:"minutes"`
	Comment  string `json:"comment"`
	LoggedAt string `json:"logged_at"`
}
