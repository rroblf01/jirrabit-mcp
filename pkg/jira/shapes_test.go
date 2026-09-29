package jira

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestShaperIssueJiraShape(t *testing.T) {
	shaper := NewShaper("https://jirrabit.example.com")
	due := "2026-10-01"
	points := 5
	issue := Issue{
		ID: 42, Key: "WEB-7", Summary: "Ship it",
		Description: "Some **bold** text",
		Status:      "In Progress", StatusID: 2, StatusCategory: "in_progress",
		Priority: "High", PriorityID: 3,
		Type: "Story", TypeID: 4,
		Project: "WEB",
		DueDate: &due, StoryPoints: &points,
		TimeSpentMinutes: 90,
	}
	assignee := "alice"
	issue.Assignee = &assignee

	out := shaper.Issue(issue)

	if out.ID != "42" || out.Key != "WEB-7" {
		t.Errorf("identity = %q/%q, want 42/WEB-7", out.ID, out.Key)
	}
	if want := "https://jirrabit.example.com/api/v1/issues/WEB-7/"; out.Self != want {
		t.Errorf("self = %q, want %q", out.Self, want)
	}
	if out.Fields.Summary != "Ship it" {
		t.Errorf("summary = %q", out.Fields.Summary)
	}
	if out.Fields.Description == nil {
		t.Fatal("description did not convert to ADF")
	}
	if out.Fields.Status == nil || out.Fields.Status.Name != "In Progress" {
		t.Fatalf("status = %+v", out.Fields.Status)
	}
	if out.Fields.Status.Category == nil || out.Fields.Status.Category.Key != "indeterminate" {
		t.Errorf("statusCategory = %+v, want key indeterminate", out.Fields.Status.Category)
	}
	if out.Fields.IssueType == nil || out.Fields.IssueType.ID != "4" {
		t.Errorf("issuetype = %+v", out.Fields.IssueType)
	}
	if out.Fields.Assignee == nil || out.Fields.Assignee.AccountID != "alice" {
		t.Errorf("assignee = %+v", out.Fields.Assignee)
	}
	// jirrabit counts minutes, Jira counts seconds.
	if out.Fields.TimeSpentSeconds != 5400 {
		t.Errorf("timespent = %d seconds, want 5400", out.Fields.TimeSpentSeconds)
	}
	if out.Fields.StoryPoints == nil || *out.Fields.StoryPoints != 5 {
		t.Errorf("story points = %v", out.Fields.StoryPoints)
	}
}

// A hand-built Issue proves the shaper, never the wire. This one goes through
// json.Unmarshal, because the bug it guards against lived entirely in a struct
// tag: TypeID was tagged `type_id` while jirrabit emits `issue_type_id`, so
// every other test in this file passed while `fields.issuetype.id` was
// permanently "" on a real response. Building the struct by hand cannot see
// that, and no amount of shaper coverage will.
func TestIssueDTOUnmarshalsJirrabitsFieldNames(t *testing.T) {
	// Field names and values as jirrabit's IssueOut actually emits them, so a
	// change on the jirrabit side has to be made here deliberately.
	body := `{
		"id": 42, "key": "WEB-7", "summary": "Ship it",
		"status": "In Progress", "status_id": 2, "status_category": "in_progress",
		"priority": "High", "priority_id": 3,
		"type": "Story", "issue_type_id": 4,
		"project": "WEB", "labels": ["backend"],
		"parent": "WEB-1", "sprint_id": 9,
		"comment_count": 2, "worklog_count": 1
	}`

	var issue Issue
	if err := json.Unmarshal([]byte(body), &issue); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Each of these was empty at some point while the response looked complete.
	for _, tc := range []struct {
		name string
		got  any
		want any
	}{
		{"issue_type_id -> TypeID", issue.TypeID, 4},
		{"status_id -> StatusID", issue.StatusID, 2},
		{"priority_id -> PriorityID", issue.PriorityID, 3},
		{"status_category", issue.StatusCategory, "in_progress"},
		{"parent", issue.Parent, "WEB-1"},
		{"comment_count", issue.CommentCount, 2},
		{"worklog_count", issue.WorklogCount, 1},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	// SprintID is a pointer, so it cannot go in the table above.
	if issue.SprintID == nil || *issue.SprintID != 9 {
		t.Errorf("sprint_id = %v, want 9", issue.SprintID)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "backend" {
		t.Errorf("labels = %v", issue.Labels)
	}

	// And the end of the chain, since a correct tag is only worth anything if
	// the value survives the shaper.
	out := NewShaper("https://x").Issue(issue)
	if out.Fields.IssueType == nil || out.Fields.IssueType.ID != "4" {
		t.Errorf("fields.issuetype.id = %+v, want 4", out.Fields.IssueType)
	}
	if out.Fields.CommentCount != 2 || out.Fields.WorklogCount != 1 {
		t.Errorf("fields.commentCount/worklogCount = %d/%d, want 2/1",
			out.Fields.CommentCount, out.Fields.WorklogCount)
	}
}

// A field jirrabit does not have must be absent, not confidently empty.
// Components shipped as [] on every issue, which reads to a model as "this
// issue has no components" — an answer jirrabit never gave.
func TestIssuePayloadOmitsComponents(t *testing.T) {
	raw, err := json.Marshal(NewShaper("https://x").Issue(Issue{ID: 1, Key: "A-1"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte(`"components"`)) {
		t.Errorf("payload claims a components field jirrabit does not have: %s", raw)
	}
}

func TestShaperIssueUnassignedIsNullNotEmpty(t *testing.T) {
	shaper := NewShaper("https://x")
	out := shaper.Issue(Issue{ID: 1, Key: "A-1", Status: "To Do"})
	if out.Fields.Assignee != nil {
		t.Errorf("unassigned issue produced %+v, want null", out.Fields.Assignee)
	}
	if out.Fields.Reporter != nil {
		t.Errorf("issue with no reporter produced %+v, want null", out.Fields.Reporter)
	}
	// Labels must serialise as [] rather than null, or a client iterating them
	// has to handle nil.
	if out.Fields.Labels == nil {
		t.Error("labels are nil; they should be an empty array")
	}
}

// A status category is omitted rather than guessed from the name: a project may
// rename "Done" to "Closed", and a wrong category is worse than a missing one.
func TestShaperOmitsUnknownStatusCategory(t *testing.T) {
	shaper := NewShaper("https://x")
	out := shaper.Issue(Issue{ID: 1, Key: "A-1", Status: "Shipped"})
	if out.Fields.Status == nil {
		t.Fatal("status missing entirely")
	}
	if out.Fields.Status.Name != "Shipped" {
		t.Errorf("name = %q", out.Fields.Status.Name)
	}
	if out.Fields.Status.Category != nil {
		t.Errorf("category = %+v, want nil for an unknown category", out.Fields.Status.Category)
	}
}

func TestStatusCategoryMapping(t *testing.T) {
	cases := map[string]string{
		"todo":          "new",
		"in_progress":   "indeterminate",
		"done":          "done",
		"backlog":       "new",
		"somethingElse": "",
	}
	for input, wantKey := range cases {
		got := statusCategory(input)
		if wantKey == "" {
			if got != nil {
				t.Errorf("statusCategory(%q) = %+v, want nil", input, got)
			}
			continue
		}
		if got == nil || got.Key != wantKey {
			t.Errorf("statusCategory(%q) = %+v, want key %q", input, got, wantKey)
		}
	}
}

func TestShaperWorklog(t *testing.T) {
	shaper := NewShaper("https://x")
	author := "bob"
	out := shaper.WorkLog(WorkLog{ID: 3, Issue: "A-1", Author: author, Minutes: 90, LoggedAt: "2026-01-01T10:00:00Z"})
	if out.TimeSpent != "1h 30m" {
		t.Errorf("timeSpent = %q, want 1h 30m", out.TimeSpent)
	}
	if out.TimeSpentSeconds != 5400 {
		t.Errorf("timeSpentSeconds = %d, want 5400", out.TimeSpentSeconds)
	}
	if out.Author == nil || out.Author.AccountID != "bob" {
		t.Errorf("author = %+v", out.Author)
	}
}

func TestShaperUserFallsBackToUsername(t *testing.T) {
	shaper := NewShaper("https://x")
	out := shaper.User(User{ID: 1, Username: "carol", Email: "c@x.com"})
	if out.DisplayName != "carol" {
		t.Errorf("displayName = %q, want the username fallback", out.DisplayName)
	}
	if shaper.User(User{Username: "dan", DisplayName: "Dan"}).DisplayName != "Dan" {
		t.Error("a real display name should win over the username")
	}
}

// statusCategory must sit inside the status object, as Jira shapes it, not
// alongside it in fields.
func TestStatusCategoryIsNestedInStatus(t *testing.T) {
	shaper := NewShaper("https://x")
	raw, err := json.Marshal(shaper.Issue(Issue{ID: 1, Key: "A-1", Status: "Done", StatusCategory: "done"}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Fields struct {
			Status struct {
				Name           string                `json:"name"`
				StatusCategory *struct{ Key string } `json:"statusCategory"`
			} `json:"status"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Fields.Status.StatusCategory == nil {
		t.Fatal("statusCategory is not nested under status")
	}
	if decoded.Fields.Status.StatusCategory.Key != "done" {
		t.Errorf("nested key = %q", decoded.Fields.Status.StatusCategory.Key)
	}
}

func TestShaperProjectsPreserveOrder(t *testing.T) {
	shaper := NewShaper("https://x")
	out := shaper.Projects([]Project{{ID: 1, Key: "AAA"}, {ID: 2, Key: "BBB"}})
	if len(out) != 2 || out[0].Key != "AAA" || out[1].Key != "BBB" {
		t.Errorf("order not preserved: %+v", out)
	}
}
