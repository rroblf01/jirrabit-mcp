package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

func argsSchema(t *testing.T, tool mcp.Tool) map[string]bool {
	t.Helper()
	props := declaredArguments(tool)
	if len(props) == 0 {
		t.Fatalf("tool %s has no readable input schema, so the strict check cannot be tested", tool.Name)
	}
	return props
}

func TestDeclaredArgumentsComeFromTheToolsOwnSchema(t *testing.T) {
	// A list of arguments maintained by hand is a list that drifts. Reading the
	// schema is what makes the check and the advertised contract the same thing.
	tool := mcp.NewTool("listJiraSprints", mcp.WithInputSchema[struct {
		schema.Target
		ProjectKeyOrID string `json:"projectKeyOrId,omitempty"`
		NotInTheSchema string `json:"somethingElse,omitempty"`
	}]())

	known := argsSchema(t, tool)
	if !known["projectKeyOrId"] {
		t.Error("projectKeyOrId should be accepted")
	}
	if !known["somethingElse"] {
		t.Error("somethingElse should be accepted, the schema is the source of truth")
	}
	if !known["instanceUrl"] || !known["apiKey"] || !known["cloudId"] {
		t.Error("the embedded Target fields should be accepted on every tool")
	}
}

func TestCheckArgumentsAcceptsWhatTheToolDeclares(t *testing.T) {
	known := map[string]bool{
		"projectKeyOrId": true, "instanceUrl": true, "apiKey": true, "cloudId": true,
	}

	if err := checkArguments("listJiraSprints", nil, known); err != nil {
		t.Errorf("a call with no arguments must pass: %v", err)
	}
	if err := checkArguments("listJiraSprints", map[string]any{}, known); err != nil {
		t.Errorf("an empty call must pass: %v", err)
	}
	if err := checkArguments("listJiraSprints", map[string]any{
		"projectKeyOrId": "DEMO", "instanceUrl": "https://x", "cloudId": "y",
	}, known); err != nil {
		t.Errorf("declared arguments must pass: %v", err)
	}
}

func TestCheckArgumentsRejectsAnUnknownArgument(t *testing.T) {
	known := map[string]bool{"projectKeyOrId": true, "instanceUrl": true, "apiKey": true}

	err := checkArguments("listJiraSprints", map[string]any{"projectKey": "DEMO"}, known)
	if err == nil {
		t.Fatal("an argument the tool does not declare must be rejected")
	}
	// The message is the interface here: an agent reads it and retries, so it
	// has to name the mistake, the alternatives and the fact that nothing ran.
	for _, want := range []string{
		"listJiraSprints",
		`"projectKey"`,
		"projectKeyOrId",
		"did you mean",
		"Nothing was changed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q; got: %v", want, err)
		}
	}
}

func TestCheckArgumentsListsEveryAcceptedName(t *testing.T) {
	known := map[string]bool{
		"projectKeyOrId": true, "sprintId": true, "instanceUrl": true,
		"apiKey": true, "cloudId": true,
	}
	err := checkArguments("listJiraSprints", map[string]any{"projectId": "3"}, known)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	// projectId is the Atlassian spelling, so a near miss is plausible; the
	// list is what saves the agent, and it must be complete.
	for _, want := range []string{"apiKey", "cloudId", "instanceUrl", "projectKeyOrId", "sprintId"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the accepted list is missing %q: %v", want, err)
		}
	}
}

func TestCheckArgumentsReportsEveryUnknownAtOnce(t *testing.T) {
	// One call naming three wrong arguments should cost one round trip, not
	// three: the agent is usually guessing several names at the same time.
	known := map[string]bool{"projectKeyOrId": true, "instanceUrl": true, "apiKey": true}
	err := checkArguments("getJiraSprint", map[string]any{
		"sprint": 4, "projectKey": "DEMO", "state": "open",
	}, known)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	for _, want := range []string{`"sprint"`, `"projectKey"`, `"state"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should list %q among the problems: %v", want, err)
		}
	}
}

func TestNearestOnlySuggestsWhenItIsClose(t *testing.T) {
	candidates := []string{"projectKeyOrId", "issueIdOrKey", "statusName", "apiKey"}

	cases := []struct {
		given string
		want  string
	}{
		{"projectKey", "projectKeyOrId"},
		{"projectkeyorid", ""}, // already right, different case: not a typo
		{"issueidorkey", ""},   // same
		{"statusname", ""},     // same
		{"issueIdOrKeyy", "issueIdOrKey"},
		{"bananas", ""},  // nothing like it, so no misleading suggestion
		{"sprintId", ""}, // already right, so nothing to suggest
	}
	for _, tc := range cases {
		if got := nearest(tc.given, candidates); got != tc.want {
			t.Errorf("nearest(%q) = %q, want %q", tc.given, got, tc.want)
		}
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"projectKey", "projectKeyOrId", 4},
		{"flaw", "lawn", 2},
	}
	for _, tc := range cases {
		if got := editDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// The failure this whole file exists for: a wrong argument name used to be
// dropped, so createJiraIssue reported success and stored no issue type.
func TestAnIssueTypeMisspellingIsCaughtRatherThanDropped(t *testing.T) {
	tool := mcp.NewTool("createJiraIssue", mcp.WithInputSchema[struct {
		schema.Target
		ProjectKey    string `json:"projectKey"`
		Summary       string `json:"summary"`
		IssueTypeName string `json:"issueTypeName,omitempty"`
	}]())
	known := argsSchema(t, tool)

	if err := checkArguments("createJiraIssue", map[string]any{
		"projectKey": "DEMO", "summary": "x", "issueType": "Task",
	}, known); err == nil {
		t.Fatal("issueType must be rejected: it is not the field, and dropping it loses the type silently")
	} else if !strings.Contains(err.Error(), "issueTypeName") {
		t.Errorf("the correction should be offered: %v", err)
	}
}

func TestDeclaredArgumentsToleratesAToolWithNoSchema(t *testing.T) {
	// A tool registered with a raw schema, or none, must not be turned into a
	// tool that rejects everything.
	raw := json.RawMessage(`{"type":"object","properties":{}}`)
	if got := declaredArguments(mcp.NewTool("raw", mcp.WithRawInputSchema(raw))); len(got) != 0 {
		t.Errorf("an empty property map should yield no names, got %v", got)
	}
	if got := declaredArguments(mcp.NewTool("noschema")); got != nil {
		t.Errorf("a tool with no schema should yield nil so the check is skipped, got %v", got)
	}
}

func TestRawInputSchemaRoundTripsThroughTheCheck(t *testing.T) {
	// Guard against the two halves drifting: what the tool advertises must be
	// exactly what the check enforces.
	tool := mcp.NewTool("getJiraIssue", mcp.WithInputSchema[struct {
		schema.Target
		IssueIDOrKey string   `json:"issueIdOrKey"`
		Fields       []string `json:"fields,omitempty"`
	}]())

	var advertised struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(tool.RawInputSchema, &advertised); err != nil {
		t.Fatalf("the schema must stay parseable: %v", err)
	}
	known := declaredArguments(tool)
	if len(known) != len(advertised.Properties) {
		t.Fatalf("the check sees %d names, the schema advertises %d", len(known), len(advertised.Properties))
	}
}
