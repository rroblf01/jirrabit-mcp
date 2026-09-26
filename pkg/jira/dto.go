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
// The fields below `Project` are not yet part of jirrabit's `IssueOut`; they
// are declared optional so that enriching the endpoint fills them in with no
// change to this repository's tool code:
//
//	Created     string   -> IssueOut.created
//	Updated     string   -> IssueOut.updated
//	StatusID    int      -> IssueOut.status_id (for status.id)
//	StatusCategory string -> IssueOut.status_category (for status.statusCategory)
//	PriorityID  int      -> IssueOut.priority_id
//	TypeID      int      -> IssueOut.issue_type_id
//	Labels      []string -> IssueOut.labels
//	Parent      string   -> IssueOut.parent (issue key of the parent)
//	SprintID    *int     -> IssueOut.sprint_id
//	Components  []string -> IssueOut.components
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

	// Optional enrichment; see the note above.
	Created        string   `json:"created"`
	Updated        string   `json:"updated"`
	StatusID       int      `json:"status_id"`
	StatusCategory string   `json:"status_category"`
	PriorityID     int      `json:"priority_id"`
	TypeID         int      `json:"type_id"`
	Labels         []string `json:"labels"`
	Parent         string   `json:"parent"`
	SprintID       *int     `json:"sprint_id"`
	Components     []string `json:"components"`
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
