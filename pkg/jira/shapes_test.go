package jira

import (
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

func TestShaperIssueUnassignedIsNullNotEmpty(t *testing.T) {
	shaper := NewShaper("https://x")
	out := shaper.Issue(Issue{ID: 1, Key: "A-1", Status: "To Do"})
	if out.Fields.Assignee != nil {
		t.Errorf("unassigned issue produced %+v, want null", out.Fields.Assignee)
	}
	if out.Fields.Reporter != nil {
		t.Errorf("issue with no reporter produced %+v, want null", out.Fields.Reporter)
	}
	// Labels and components must serialise as [] rather than null, or a client
	// iterating them has to handle nil.
	if out.Fields.Labels == nil || out.Fields.Components == nil {
		t.Error("labels/components are nil; they should be empty arrays")
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
