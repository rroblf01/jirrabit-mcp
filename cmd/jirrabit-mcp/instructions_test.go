package main

import (
	"strings"
	"testing"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
)

// These instructions are the only documentation an agent is guaranteed to read.
// A sentence that is wrong for the server it arrived at costs a failed call, and
// the agent gives up rather than reading an error as a configuration problem.
func TestInstanceChoiceDescribesTheServerItIsRunningOn(t *testing.T) {
	cases := []struct {
		name       string
		defaultURL string
		allowed    *jira.HostPolicy
		must       []string
		mustNot    []string
		why        string
	}{
		{
			name:    "a shared server with no default",
			why:     "the case that matters: promising a default that does not exist breaks the first call",
			must:    []string{"MUST pass both", "no default instance"},
			mustNot: []string{"you may omit both", "has a default instance"},
		},
		{
			name:       "a personal server with a default and no allowlist",
			defaultURL: "http://localhost:8000",
			why:        "the convenience case, still able to reach another instance",
			must:       []string{"default instance", "you may omit both", "Pass them anyway"},
		},
		{
			name:       "a public server with a default and an allowlist",
			defaultURL: "https://jirrabit.example.com",
			allowed:    jira.NewHostPolicy(".example.com"),
			why:        "the default does not mean every instance is reachable",
			must:       []string{"default instance", "allowlist", "refused"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := instanceChoice(tc.defaultURL, tc.allowed)
			for _, want := range tc.must {
				if !strings.Contains(got, want) {
					t.Errorf("the text should contain %q — %s; got: %s", want, tc.why, got)
				}
			}
			for _, unwanted := range tc.mustNot {
				if strings.Contains(got, unwanted) {
					t.Errorf("the text should not contain %q — %s; got: %s", unwanted, tc.why, got)
				}
			}
			// An agent reads this as prose, so a stray placeholder would be worse
			// than a missing sentence.
			if strings.Contains(got, "__") {
				t.Errorf("the text still has a placeholder in it: %s", got)
			}
		})
	}
}

func TestInstanceChoiceQuotesTheErrorItPredicts(t *testing.T) {
	// The sentence quotes the server's own error, so an agent can match the two
	// and know the problem is configuration rather than a bad key.
	got := instanceChoice("", nil)
	if !strings.Contains(got, "no instance given and this server has no default") {
		t.Errorf("the predicted error should be quoted verbatim so it can be recognised: %s", got)
	}
}

// This one is the test the first implementation was missing.
//
// It calls buildInstructions, the function the server calls, rather than
// performing its own substitution. The earlier version did the substitution
// inside the test, passed, and shipped a server that told agents to look for a
// literal "__INSTANCE_CHOICE__" in its instructions.
func TestBuildInstructionsNeverShipsThePlaceholder(t *testing.T) {
	if got := strings.Count(instructions, placeholder); got != 1 {
		t.Fatalf("the instructions must contain the marker exactly once, found %d. "+
			"If it is gone, instanceChoice has nothing to fill in and this test is vacuous", got)
	}

	cases := []struct {
		defaultURL string
		allowed    *jira.HostPolicy
		must       string
	}{
		{"", nil, "MUST pass both"},
		{"http://localhost:8000", nil, "you may omit both"},
		{"https://jirrabit.example.com", jira.NewHostPolicy(".example.com"), "allowlist"},
	}
	for _, tc := range cases {
		text := buildInstructions(tc.defaultURL, tc.allowed)
		if strings.Contains(text, placeholder) {
			t.Errorf("defaultURL=%q: the placeholder reached the agent:\n%s", tc.defaultURL, text[:200])
		}
		if !strings.Contains(text, tc.must) {
			t.Errorf("defaultURL=%q: the filled-in sentence is missing %q", tc.defaultURL, tc.must)
		}
		// The paragraph has to stay inside its section, and the surrounding prose
		// has to survive it.
		if !strings.Contains(text, "CHOOSING AN INSTANCE") {
			t.Error("the section header should survive the substitution")
		}
		if !strings.Contains(text, "An apiKey without an instanceUrl is rejected") {
			t.Error("the rest of the paragraph should survive the substitution")
		}
	}
}

// The filled sentence replaces the marker in place, so the block reads as prose.
// The first attempt concatenated instead, producing "...stores
// neither.You are working with jirrabit" in a single paragraph.
func TestTheFilledSentenceIsNotGluedToTheNextLine(t *testing.T) {
	text := buildInstructions("", nil)
	if strings.Contains(text, "stores neither.You") {
		t.Error("the instance sentence runs into the opening line with no separator")
	}
	if !strings.Contains(text, "This server has no default instance") {
		t.Fatal("the instance sentence should be present")
	}
	// The filled sentence lands in its own paragraph, between the two that
	// surround the marker, rather than welded to either of them.
	if !strings.Contains(text, "this server:\n\nThis server has no default instance") {
		t.Error("the filled sentence should start its own paragraph after the one introducing it")
	}
	if !strings.Contains(text, "stores neither.\n\nAn apiKey without an instanceUrl is rejected") {
		t.Error("the filled sentence should end its own paragraph before the next one")
	}
}
