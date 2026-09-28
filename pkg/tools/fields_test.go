package tools

import (
	"strings"
	"testing"

	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// The free-form `fields` object is advertised in the tool description, so every
// aliased key has to be reachable through it. The duplicate check used to fire on
// a static list of arguments the tool *has* rather than the ones the caller
// *sent*, so `fields: {"story_points": 5}` was rejected as a duplicate of
// storyPoints even with no storyPoints in the call. Every aliased key was
// unreachable, and no test looked, because the strict-argument check only sees
// that `fields` itself is declared.
func TestFieldsAcceptsAKeyWhoseNamedArgumentWasNotSent(t *testing.T) {
	args := schema.EditIssueArgs{Summary: strPtr("nuevo")} // summary only
	clean, err := normaliseFields(
		map[string]any{"story_points": 5, "labels": []any{"backend"}, "epic_id": 7},
		editProvidedNames(args),
	)
	if err != nil {
		t.Fatalf("a key with no matching named argument was refused: %v", err)
	}
	if clean["story_points"] != 5 {
		t.Errorf("story_points = %v", clean["story_points"])
	}
	if got := clean["epic_id"]; got != 7 {
		t.Errorf("epic_id = %v, want 7", got)
	}
}

// The check is still worth having: sending the same thing twice leaves the
// caller unable to tell which value was applied.
func TestFieldsRejectsAGenuineDuplicate(t *testing.T) {
	args := schema.EditIssueArgs{StoryPoints: intPtr(3)}
	_, err := normaliseFields(map[string]any{"story_points": 5}, editProvidedNames(args))
	if err == nil {
		t.Fatal("a real duplicate was accepted")
	}
	if !strings.Contains(err.Error(), "pass it once") {
		t.Errorf("unhelpful refusal: %v", err)
	}
}

// A pointer argument explicitly set to its zero value is still "sent": passing
// storyPoints: 0 is not the same as omitting it.
func TestFieldsTreatsAZeroPointerAsSent(t *testing.T) {
	zero := 0
	args := schema.EditIssueArgs{StoryPoints: &zero}
	_, err := normaliseFields(map[string]any{"story_points": 5}, editProvidedNames(args))
	if err == nil {
		t.Error("storyPoints: 0 and fields.story_points were both accepted, so neither wins visibly")
	}
}

// An empty string for `parent` means "detach", so it counts as sent.
func TestFieldsTreatsAnEmptyParentAsSent(t *testing.T) {
	empty := ""
	args := schema.EditIssueArgs{Parent: &empty}
	_, err := normaliseFields(map[string]any{"parent": "WEB-1"}, editProvidedNames(args))
	if err == nil {
		t.Error("an empty parent and fields.parent were both accepted")
	}
}

// The camelCase spellings resolve to the same canonical names, so an agent
// writing `storyPoints` in the free-form object is not told it is wrong.
func TestFieldAliasesCoverBothSpellings(t *testing.T) {
	for _, spelling := range []string{"story_points", "storyPoints", "STORYPOINTS"} {
		clean, err := normaliseFields(
			map[string]any{spelling: 1}, map[string]bool{},
		)
		if err != nil {
			t.Errorf("fields[%q] refused: %v", spelling, err)
			continue
		}
		if clean["story_points"] != 1 {
			t.Errorf("fields[%q] became %v, want the canonical story_points", spelling, clean)
		}
	}
}

// Every alias must be a key the REST API actually accepts, or the adapter would
// forward something jirrabit rejects with an opaque 422.
func TestFieldAliasesMapToRealAPIFields(t *testing.T) {
	want := map[string]bool{
		"status_id": true, "priority_id": true, "issue_type_id": true, "sprint_id": true,
		"assignee_id": true, "story_points": true, "due_date": true,
		"epic_id": true, "parent": true, "labels": true,
		"estimate_minutes": true, "time_remaining_minutes": true,
	}
	for _, canonical := range fieldAliases {
		if !want[canonical] {
			t.Errorf("fieldAliases maps to %q, which is not a jirrabit write field", canonical)
		}
	}
	usage := fieldUsage()
	for key := range want {
		if !strings.Contains(usage, key) {
			t.Errorf("fieldUsage omits %q, so the error message will not list it", key)
		}
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
