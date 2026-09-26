package jira

import "fmt"

// Shaper converts jirrabit's DTOs into the payload shapes a Jira-trained agent
// expects. It holds the base URL so it can populate each resource's `self`.
//
// A Shaper is read-only after construction and safe for concurrent use.
type Shaper struct {
	baseURL string
}

// NewShaper returns a Shaper that points `self` links at the given instance.
func NewShaper(baseURL string) *Shaper {
	return &Shaper{baseURL: baseURL}
}

// --- Jira resource shapes --------------------------------------------------

// IssueResource is a Jira issue: an id, a key, a self link and a `fields` map.
// The nesting matters — agents parse `fields.status.name`, not `status`.
type IssueResource struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Self   string      `json:"self"`
	Fields IssueFields `json:"fields"`
}

// IssueFields is a Jira issue's `fields` object.
type IssueFields struct {
	Summary     string           `json:"summary"`
	Description *ADFDoc          `json:"description,omitempty"`
	Status      *StatusResource  `json:"status,omitempty"`
	Priority    *NamedID         `json:"priority,omitempty"`
	IssueType   *NamedID         `json:"issuetype,omitempty"`
	Project     *ProjectResource `json:"project,omitempty"`
	Assignee    *UserResource    `json:"assignee,omitempty"`
	Reporter    *UserResource    `json:"reporter,omitempty"`
	Labels      []string         `json:"labels"`
	Components  []string         `json:"components"`
	DueDate     *string          `json:"duedate,omitempty"`
	Created     string           `json:"created,omitempty"`
	Updated     string           `json:"updated,omitempty"`
	// TimeSpent is "timeSpentSeconds" in Jira's API. jirrabit stores minutes.
	TimeSpentSeconds int `json:"timespent,omitempty"`
	// TimeOriginalEstimate maps to jirrabit's estimate_minutes.
	TimeOriginalEstimate int `json:"timeoriginalestimate,omitempty"`
	// StoryPoints is Jira's default custom field for story points.
	StoryPoints *int `json:"customfield_10016,omitempty"`
	// ParentKey is the parent's issue key, so an agent can walk subtasks up.
	ParentKey string `json:"parentKey,omitempty"`
	// SprintID mirrors Jira's sprint field for Agile-trained agents.
	SprintID *int `json:"sprintId,omitempty"`
}

// NamedID is Jira's shape for a status, priority or issue type: an id and a
// name. Jira nests a statusCategory inside a status; see StatusResource.
type NamedID struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// StatusResource is a Jira status with its category.
type StatusResource struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Category *StatusCategory `json:"statusCategory,omitempty"`
}

// StatusCategory is Jira's todo / in-progress / done grouping. Agents branch on
// it constantly, most often via the JQL fragment `statusCategory != Done`.
type StatusCategory struct {
	ID   int    `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// UserResource is a Jira user object, keyed by accountId. jirrabit identifies users by
// username today; the username stands in for the accountId until jirrabit
// exposes stable account ids of its own.
type UserResource struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       bool   `json:"active"`
}

// ProjectResource is a Jira project (Atlassian calls it a "space").
type ProjectResource struct {
	ID             string `json:"id"`
	Key            string `json:"key"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	ProjectTypeKey string `json:"projectTypeKey"`
}

// CommentResource is a Jira comment.
type CommentResource struct {
	ID       string        `json:"id"`
	Self     string        `json:"self"`
	Body     *ADFDoc       `json:"body,omitempty"`
	Author   *UserResource `json:"author,omitempty"`
	Created  string        `json:"created"`
	Updated  string        `json:"updated,omitempty"`
	IssueKey string        `json:"issueKey,omitempty"`
}

// WorkLogResource is a Jira worklog, in Jira's time format.
type WorkLogResource struct {
	ID               string        `json:"id"`
	Self             string        `json:"self"`
	IssueKey         string        `json:"issueKey"`
	Author           *UserResource `json:"author,omitempty"`
	TimeSpent        string        `json:"timeSpent"`
	TimeSpentSeconds int           `json:"timeSpentSeconds"`
	Comment          *ADFDoc       `json:"comment,omitempty"`
	Started          string        `json:"started"`
}

// SprintResource is a Jira sprint.
type SprintResource struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Goal          string  `json:"goal,omitempty"`
	State         string  `json:"state"`
	StartDate     *string `json:"startDate,omitempty"`
	EndDate       *string `json:"endDate,omitempty"`
	OriginBoardID *int    `json:"originBoardId,omitempty"`
	CompleteDate  *string `json:"completeDate,omitempty"`
}

// --- conversions -----------------------------------------------------------

// Issue renders a jirrabit issue as a Jira issue resource.
func (s *Shaper) Issue(i Issue) IssueResource {
	fields := IssueFields{
		Summary:     i.Summary,
		Description: ADFText(i.Description),
		Priority:    &NamedID{ID: intToID(i.PriorityID), Name: i.Priority},
		IssueType:   &NamedID{ID: intToID(i.TypeID), Name: i.Type},
		Project:     &ProjectResource{Key: i.Project, Name: i.Project, ProjectTypeKey: "software"},
		Assignee:    userFromUsername(i.Assignee),
		Reporter:    userFromUsername(i.Reporter),
		Labels:      nonNilStrings(i.Labels),
		Components:  nonNilStrings(i.Components),
		DueDate:     i.DueDate,
		Created:     i.Created,
		Updated:     i.Updated,
		StoryPoints: i.StoryPoints,
		ParentKey:   i.Parent,
		SprintID:    i.SprintID,
	}
	// Time is in minutes in jirrabit, seconds in Jira.
	fields.TimeSpentSeconds = i.TimeSpentMinutes * 60
	if i.EstimateMinutes != nil {
		fields.TimeOriginalEstimate = *i.EstimateMinutes * 60
	}
	if status := s.status(i); status != nil {
		fields.Status = status
	}
	return IssueResource{
		ID:     intToID(i.ID),
		Key:    i.Key,
		Self:   fmt.Sprintf("%s/api/v1/issues/%s/", s.baseURL, i.Key),
		Fields: fields,
	}
}

// status builds a Jira status, including its category when jirrabit reports
// one. The category is omitted rather than guessed: agents rely on
// `statusCategory`, but inventing a value from the status *name* would be
// wrong for a project that renames "Done" to "Closed".
func (s *Shaper) status(i Issue) *StatusResource {
	if i.Status == "" {
		return nil
	}
	out := &StatusResource{ID: intToID(i.StatusID), Name: i.Status}
	if category := statusCategory(i.StatusCategory); category != nil {
		out.Category = category
	}
	return out
}

// statusCategory maps jirrabit's Status.category onto Jira's statusCategory.
// jirrabit already uses Jira's three categories (todo, in_progress, done).
func statusCategory(category string) *StatusCategory {
	switch category {
	case "todo", "new", "to_do", "backlog":
		return &StatusCategory{ID: 2, Key: "new", Name: "To Do"}
	case "in_progress", "indeterminate":
		return &StatusCategory{ID: 4, Key: "indeterminate", Name: "In Progress"}
	case "done", "complete", "closed":
		return &StatusCategory{ID: 3, Key: "done", Name: "Done"}
	}
	return nil
}

// Issues renders a list of issues, preserving order.
func (s *Shaper) Issues(items []Issue) []IssueResource {
	out := make([]IssueResource, 0, len(items))
	for _, item := range items {
		out = append(out, s.Issue(item))
	}
	return out
}

// User renders a jirrabit user as a Jira user.
func (s *Shaper) User(u User) UserResource {
	return UserResource{
		AccountID:    u.Username,
		DisplayName:  displayNameOr(u.DisplayName, u.Username),
		EmailAddress: u.Email,
		Active:       true,
	}
}

// Project renders a jirrabit project as a Jira project.
func (s *Shaper) Project(p Project) ProjectResource {
	return ProjectResource{
		ID:             intToID(p.ID),
		Key:            p.Key,
		Name:           p.Name,
		Description:    p.Description,
		ProjectTypeKey: "software",
	}
}

// Projects renders a list of projects, preserving order.
func (s *Shaper) Projects(items []Project) []ProjectResource {
	out := make([]ProjectResource, 0, len(items))
	for _, item := range items {
		out = append(out, s.Project(item))
	}
	return out
}

// Comment renders a jirrabit comment as a Jira comment.
func (s *Shaper) Comment(c Comment) CommentResource {
	return CommentResource{
		ID:       intToID(c.ID),
		Self:     fmt.Sprintf("%s/api/v1/issues/%s/comments/%d/", s.baseURL, c.Issue, c.ID),
		Body:     ADFText(c.Body),
		Author:   userFromUsername(&c.Author),
		Created:  c.CreatedAt,
		IssueKey: c.Issue,
	}
}

// Comments renders a list of comments, preserving order.
func (s *Shaper) Comments(items []Comment) []CommentResource {
	out := make([]CommentResource, 0, len(items))
	for _, item := range items {
		out = append(out, s.Comment(item))
	}
	return out
}

// WorkLog renders a jirrabit worklog as a Jira worklog. jirrabit stores
// minutes; Jira sends "2h 30m" alongside a seconds count, so both are produced.
func (s *Shaper) WorkLog(w WorkLog) WorkLogResource {
	return WorkLogResource{
		ID:               intToID(w.ID),
		Self:             fmt.Sprintf("%s/api/v1/issues/%s/worklogs/%d/", s.baseURL, w.Issue, w.ID),
		IssueKey:         w.Issue,
		Author:           userFromUsername(&w.Author),
		TimeSpent:        formatDuration(w.Minutes),
		TimeSpentSeconds: w.Minutes * 60,
		Comment:          ADFText(w.Comment),
		Started:          w.LoggedAt,
	}
}

// WorkLogs renders a list of worklogs, preserving order.
func (s *Shaper) WorkLogs(items []WorkLog) []WorkLogResource {
	out := make([]WorkLogResource, 0, len(items))
	for _, item := range items {
		out = append(out, s.WorkLog(item))
	}
	return out
}

// Sprint renders a jirrabit sprint as a Jira sprint. Jira calls a sprint's
// lifecycle field `state`; jirrabit calls it `status` with the same values.
func (s *Shaper) Sprint(sp Sprint) SprintResource {
	return SprintResource{
		ID:        sp.ID,
		Name:      sp.Name,
		Goal:      sp.Goal,
		State:     sp.Status,
		StartDate: sp.StartDate,
		EndDate:   sp.EndDate,
	}
}

// Sprints renders a list of sprints, preserving order.
func (s *Shaper) Sprints(items []Sprint) []SprintResource {
	out := make([]SprintResource, 0, len(items))
	for _, item := range items {
		out = append(out, s.Sprint(item))
	}
	return out
}

// --- helpers ---------------------------------------------------------------

// userFromUsername builds a Jira user from a username, or nil when unset.
// A nil pointer in, a nil pointer out: an unassigned issue must report
// `"assignee": null`, not a user object with empty fields.
func userFromUsername(username *string) *UserResource {
	if username == nil || *username == "" {
		return nil
	}
	return &UserResource{AccountID: *username, DisplayName: *username, Active: true}
}

func displayNameOr(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

func intToID(id int) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprint(id)
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// formatDuration renders minutes in Jira's "2h 30m" style, which omits empty
// units so 90 minutes is "1h 30m" and 120 is "2h" rather than "2h 0m".
func formatDuration(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}
	hours, mins := minutes/60, minutes%60
	switch {
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
