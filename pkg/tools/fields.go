package tools

import (
	"encoding/base64"
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
	"statusid":               "status_id",
	"status_id":              "status_id",
	"priorityid":             "priority_id",
	"priority_id":            "priority_id",
	"issuetypeid":            "issue_type_id",
	"issue_type_id":          "issue_type_id",
	"sprintid":               "sprint_id",
	"sprint_id":              "sprint_id",
	"assigneeid":             "assignee_id",
	"assignee_id":            "assignee_id",
	"storypoints":            "story_points",
	"story_points":           "story_points",
	"duedate":                "due_date",
	"due_date":               "due_date",
	"epicid":                 "epic_id",
	"epic_id":                "epic_id",
	"parent":                 "parent",
	"labels":                 "labels",
	"label":                  "labels",
	"estimateminutes":        "estimate_minutes",
	"estimate_minutes":       "estimate_minutes",
	"originalestimate":       "estimate_minutes",
	"timereaminingminutes":   "time_remaining_minutes",
	"time_remaining_minutes": "time_remaining_minutes",
	"remainingestimate":      "time_remaining_minutes",
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
//
// `provided` must be the arguments the caller *actually* sent, not the set of
// arguments the tool happens to have. Those are different things, and confusing
// them made the free-form object unusable: with a static list, sending
// `fields: {"story_points": 5}` was rejected as a duplicate of `storyPoints`
// whether or not `storyPoints` was in the call, so every aliased key was
// unreachable through the field the tool's own description advertises. The
// strict-argument check would have caught it, except that `fields` is a declared
// argument and its contents are this function's business.
func normaliseFields(raw map[string]any, provided map[string]bool) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	clean := make(map[string]any, len(raw))
	for key, value := range raw {
		canonical, known := fieldAliases[strings.ToLower(strings.TrimSpace(key))]
		if !known {
			return nil, fmt.Errorf(
				"fields[%q] is not something this server can set. Accepted keys: %s. "+
					"Ids come from listJiraIssueTypeMetadata, listJiraPriorities, listJiraStatuses and "+
					"listJiraEpics. Labels are plain names and are created on demand; `parent` is an issue "+
					"key; the estimate fields are in minutes",
				key, fieldUsage())
		}
		if provided[canonical] {
			return nil, fmt.Errorf(
				"fields[%q] duplicates an explicit argument for the same thing; pass it once, "+
					"either as the named argument or in fields", key)
		}
		clean[canonical] = value
	}
	return clean, nil
}

// providedNames builds the "actually sent" set for a create call. Only arguments
// that can be told apart from absent count: a pointer is nil or not, a string is
// empty or not. The two plain strings that are always in the payload (summary,
// and description when non-empty) are handled by the caller.
func providedNames(names map[string]bool) map[string]bool {
	out := make(map[string]bool, len(names))
	for name := range names {
		out[name] = true
	}
	return out
}

// base64DecodedLen reports how many bytes a base64 string holds, without
// keeping a copy of them.
//
// Used to refuse an oversized attachment before pushing 6.7 MB of base64 at an
// instance that will only reject it at the far end. validate=True on the way,
// because the lenient form silently drops characters and would report a length
// for something that is not the file.
func base64DecodedLen(encoded string) (int, error) {
	clean := strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(encoded)
	raw, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		// Retry without padding: some encoders omit it and a file that is
		// otherwise fine should not be refused over three '=' characters.
		raw, err = base64.RawStdEncoding.DecodeString(clean)
		if err != nil {
			return 0, err
		}
	}
	return len(raw), nil
}
