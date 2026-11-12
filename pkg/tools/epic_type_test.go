package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestEpicTypeErrorRefusesTheEpicCategory(t *testing.T) {
	types := []issueTypeDTO{
		{ID: 1, Name: "Epic", Category: "epic"},
		{ID: 3, Name: "Task", Category: "task"},
	}
	err := epicTypeError(types, 1)
	if err == nil {
		t.Fatal("an epic-category type must be refused: it creates an epic no epic list shows")
	}
	if !errors.Is(err, errEpicIssueType) || !strings.Contains(err.Error(), "createJiraEpic") || !strings.Contains(err.Error(), "epicId") {
		t.Errorf("the refusal should name the right tool and argument: %v", err)
	}
	if err := epicTypeError(types, 3); err != nil {
		t.Errorf("a task must pass: %v", err)
	}
	if err := epicTypeError(types, 99); err != nil {
		t.Errorf("an unknown id is jirrabit's to reject, not this check's: %v", err)
	}
}

func TestIntValueReadsTypedAndDecodedIDs(t *testing.T) {
	for _, value := range []any{1, float64(1)} {
		if id, ok := intValue(value); !ok || id != 1 {
			t.Errorf("intValue(%#v) = %d, %v", value, id, ok)
		}
	}
	for _, value := range []any{nil, "1", 1.5} {
		if _, ok := intValue(value); ok {
			t.Errorf("intValue(%#v) should not be an id", value)
		}
	}
}

func TestAnnotateIssueTypesFlagsOnlyEpics(t *testing.T) {
	got := annotateIssueTypes([]issueTypeDTO{
		{ID: 1, Name: "Epic", Category: "epic"},
		{ID: 3, Name: "Task", Category: "task"},
	})
	if !strings.Contains(got[0].Note, "createJiraEpic") {
		t.Errorf("the epic type should point at createJiraEpic: %q", got[0].Note)
	}
	if got[1].Note != "" {
		t.Errorf("a task should carry no note: %q", got[1].Note)
	}
}
