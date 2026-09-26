package tools

import (
	"fmt"
	"sort"
	"strings"
)

// JQL vocabulary, kept next to the field mapping because both describe what
// jirrabit accepts. The server's instructions quote this so an agent can write a
// valid query without reading the README.

const (
	// JQLFieldList and JQLOperatorList are quoted in the server's instructions.
	// They live here, next to the alias table that implements them, so the text
	// an agent reads cannot drift away from what the parser accepts.
	JQLFieldList = "project, key, status, statusCategory, priority, type, label, " +
		"sprint, epic, assignee, reporter, text"
	JQLOperatorList = "=, !=, ~, in, plus 'is EMPTY' and 'is not EMPTY'"
)

// fieldAliases maps the free-form `fields` object onto the numeric ids jirrabit's
// write endpoints take.
//
// Two spellings are accepted per key: the snake_case the REST API uses, and the
// camelCase an agent is likely to reach for. This is deliberately explicit —
// the alternative, ignoring an unrecognised key, means an agent gets a 200 and a
// silently wrong result, which is the failure mode this mapping exists to
// prevent.
var fieldAliases = map[string]string{
	"statusid":      "status_id",
	"status_id":     "status_id",
	"priorityid":    "priority_id",
	"priority_id":   "priority_id",
	"issuetypeid":   "issue_type_id",
	"issue_type_id": "issue_type_id",
	"sprintid":      "sprint_id",
	"sprint_id":     "sprint_id",
	"assigneeid":    "assignee_id",
	"assignee_id":   "assignee_id",
	"storypoints":   "story_points",
	"story_points":  "story_points",
	"duedate":       "due_date",
	"due_date":      "due_date",
}

// fieldUsage renders the accepted keys for an error message, sorted so the
// message is stable.
func fieldUsage() string {
	seen := map[string]bool{}
	for _, canonical := range fieldAliases {
		seen[canonical] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// normaliseFields validates and canonicalises a free-form `fields` object.
//
// An unrecognised key is an error rather than a no-op, and so is a key that
// duplicates a named argument: silently letting one win would leave the caller
// unable to tell which value was applied.
func normaliseFields(raw map[string]any, named map[string]bool) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	clean := make(map[string]any, len(raw))
	for key, value := range raw {
		canonical, known := fieldAliases[strings.ToLower(strings.TrimSpace(key))]
		if !known {
			return nil, fmt.Errorf(
				"fields[%q] is not something this server can set. Accepted keys: %s. "+
					"Ids come from listJiraIssueTypeMetadata, listJiraPriorities and "+
					"listJiraStatuses",
				key, fieldUsage())
		}
		if named[canonical] {
			return nil, fmt.Errorf(
				"fields[%q] duplicates an explicit argument for the same thing; pass it once, "+
					"either as the named argument or in fields", key)
		}
		clean[canonical] = value
	}
	return clean, nil
}
