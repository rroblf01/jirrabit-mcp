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

func TestThePlaceholderIsAlwaysReplaced(t *testing.T) {
	// If the placeholder ever ships unresolved, the instructions tell the agent
	// to look for a literal marker in its own instructions.
	for _, url := range []string{"", "http://localhost:8000"} {
		text := strings.Replace(instructions, "__INSTANCE_CHOICE__", instanceChoice(url, nil), 1)
		if strings.Contains(text, "__INSTANCE_CHOICE__") {
			t.Fatalf("placeholder left in the instructions for defaultURL=%q", url)
		}
		if !strings.Contains(text, "CHOOSING AN INSTANCE") {
			t.Fatalf("the section header should survive the substitution")
		}
	}
}
