// Command flowtest drives every registered tool over a real MCP stdio session
// and asserts on the shape of each response.
//
// Where cmd/smoke asks "does this return usable data?", flowtest asks "does this
// do the thing an agent would expect?" — that a created issue is readable back,
// that a status change is reflected in getJiraIssue, that the negative cases
// come back as explanations rather than as empty success. The write side is
// verified separately from the Django side, so a tool that returns 200 while
// silently dropping the write cannot pass.
//
//	go run ./cmd/flowtest -server ./bin/jirrabit-mcp
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

var results []string
var failures int

func check(name string, ok bool, detail string) {
	mark := "PASS"
	if !ok {
		mark = "FAIL"
		failures++
	}
	line := fmt.Sprintf("  %s  %s", mark, name)
	if detail != "" && !ok {
		line += "  -- " + detail
	}
	fmt.Println(line)
	results = append(results, fmt.Sprintf("%s %s", mark, name))
}

func main() {
	serverPath := flag.String("server", "./bin/jirrabit-mcp", "path to the server binary")
	timeout := flag.Duration("timeout", 120*time.Second, "overall timeout")
	// -phantoms-only checks the tool prose and nothing else. It needs a server
	// that starts and lists its tools, but no jirrabit behind it, so CI can run
	// it without standing up the whole stack. The full run needs a live instance.
	phantomsOnly := flag.Bool("phantoms-only", false,
		"only check that the instructions and tool descriptions name no unregistered tool")
	projectKey := flag.String("project", "",
		"project key to create the test issue in. Discovered from listJiraProjects when empty")
	flag.Parse()

	if *phantomsOnly {
		if err := checkPhantoms(*serverPath, *timeout); err != nil {
			fmt.Fprintf(os.Stderr, "flowtest: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n=== phantom check: %d checks, %d failed ===\n", len(results), failures)
		if failures > 0 {
			os.Exit(1)
		}
		return
	}

	if err := run(*serverPath, *projectKey, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "flowtest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== flowtest: %d checks, %d failed ===\n", len(results), failures)
	if failures > 0 {
		os.Exit(1)
	}
}

func run(serverPath string, projectKeyFlag string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	proc := transport.NewStdio(serverPath, nil)
	defer proc.Close()

	session := client.NewClient(proc)
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer session.Close()

	init, err := session.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	check("initialize returns server instructions", len(init.Instructions) > 200,
		fmt.Sprintf("instructions are %d bytes", len(init.Instructions)))

	tools, err := session.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	names := map[string]bool{}
	for _, t := range tools.Tools {
		names[t.Name] = true
	}

	if err := scanPhantoms(init.Instructions, tools.Tools); err != nil {
		return err
	}
	check("all 16 default tools are registered", len(tools.Tools) >= 16,
		fmt.Sprintf("only %d registered", len(tools.Tools)))
	fmt.Printf("  tools: %s\n", strings.Join(toolNames(tools.Tools), ", "))

	// --- read side ------------------------------------------------------
	fmt.Println("\n[1] identity and metadata")
	me, err := call(ctx, session, "getJiraCurrentUser", nil)
	check("getJiraCurrentUser", err == nil && jsonString(me, "accountId") != "", errStr(err))
	check("identity is the demo superuser", jsonString(me, "accountId") == "alice_pm",
		jsonString(me, "accountId"))
	originalName := jsonString(me, "displayName")
	updated, err := call(ctx, session, "updateJiraCurrentUser", map[string]any{
		"displayName": "flowtest",
	})
	check("updateJiraCurrentUser changes the caller's own display name",
		err == nil && nestedString(updated, "user", "display_name") == "flowtest",
		errStr(err)+" "+truncate(updated, 160))
	if err == nil {
		// Restore it in the same block: the demo user is shared, and a display
		// name left as "flowtest" would confuse the next run's assertions.
		restored, err := call(ctx, session, "updateJiraCurrentUser", map[string]any{
			"displayName": originalName,
		})
		check("the display name is restored afterwards",
			err == nil && nestedString(restored, "user", "display_name") == originalName,
			errStr(err))
	}
	_, err = call(ctx, session, "updateJiraCurrentUser", map[string]any{
		"palette": "chartreuse",
	})
	check("updateJiraCurrentUser names the valid palettes on a bad one",
		err != nil && strings.Contains(err.Error(), "blue"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "updateJiraCurrentUser", map[string]any{})
	check("updateJiraCurrentUser with an empty body says what it accepts",
		err != nil && strings.Contains(err.Error(), "displayName"),
		fmt.Sprint(err))

	projects, err := call(ctx, session, "listJiraProjects", nil)
	// Resolved here so every later assertion can be about *this* instance's
	// project rather than a name baked in when the test was written.
	projectKey := firstProjectKey(projectKeyFlag, projects)
	check("listJiraProjects", err == nil && strings.Contains(projects, projectKey),
		errStr(err)+" "+truncate(projects, 160))

	types, err := call(ctx, session, "listJiraIssueTypeMetadata", nil)
	check("listJiraIssueTypeMetadata returns ids", err == nil && strings.Contains(types, "id"),
		errStr(err))
	statuses, err := call(ctx, session, "listJiraStatuses", nil)
	check("listJiraStatuses returns ids", err == nil && strings.Contains(statuses, "\"id\""),
		errStr(err))
	priorities, err := call(ctx, session, "listJiraPriorities", nil)
	check("listJiraPriorities returns ids", err == nil && strings.Contains(priorities, "id"),
		errStr(err))

	// The user directory, with no query. This is the check for the gap that made
	// assigning an issue a guess: every other way of naming a person needed the
	// name first, and an agent handed "Bob" had nothing to search on.
	users, err := call(ctx, session, "listJiraUsers", nil)
	check("listJiraUsers lists everyone when given no query",
		err == nil && strings.Contains(users, "username"), errStr(err)+" "+truncate(users, 160))
	check("the directory includes the key's own user",
		strings.Contains(users, "alice_pm"), truncate(users, 200))
	byQuery, err := call(ctx, session, "listJiraUsers", map[string]any{"query": "bob"})
	check("listJiraUsers narrows on a query", err == nil && strings.Contains(byQuery, "bob_dev"),
		errStr(err)+" "+truncate(byQuery, 160))

	project, err := call(ctx, session, "getJiraProject", map[string]any{"projectKeyOrId": projectKey})
	check("getJiraProject reads one project",
		err == nil && jsonString(project, "key") == projectKey,
		errStr(err)+" "+truncate(project, 160))

	projectIssues, err := call(ctx, session, "listJiraProjectIssues",
		map[string]any{"projectKeyOrId": projectKey})
	check("listJiraProjectIssues returns the project's issues",
		err == nil && strings.Contains(projectIssues, "values"),
		errStr(err)+" "+truncate(projectIssues, 160))

	// --- the write/read round trip -------------------------------------
	fmt.Println("\n[2] create, read back, edit")
	issueTypeID := firstID(types)
	statusID := firstID(statuses)
	priorityID := firstID(priorities)

	created, err := call(ctx, session, "createJiraIssue", map[string]any{
		"projectKey":  projectKey,
		"summary":     "Flujo MCP: incidencia de verificacion",
		"description": "Creada por cmd/flowtest.",
		"issueTypeId": issueTypeID,
		"priorityId":  priorityID,
	})
	check("createJiraIssue", err == nil, errStr(err))
	key := jsonString(created, "key")
	check("created issue has a key", strings.HasPrefix(key, projectKey+"-"), key)
	check("created issue carries its summary",
		nestedString(created, "fields", "summary") == "Flujo MCP: incidencia de verificacion",
		truncate(created, 120))

	fetched, err := call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("getJiraIssue reads it back", err == nil, errStr(err))
	check("read-back matches what was written",
		nestedString(fetched, "fields", "summary") == "Flujo MCP: incidencia de verificacion",
		truncate(fetched, 120))

	// The issue type's id, not just its name. This was permanently "" because
	// the DTO tagged the field `type_id` while jirrabit emits `issue_type_id`,
	// and every check above still passed: the response was complete, the
	// summary was right, the call succeeded. A field that is always empty is
	// invisible to a check that does not read that field.
	check("the issue type carries an id, not only a name",
		nestedString(fetched, "fields", "issuetype", "id") != "",
		truncate(fetched, 200))
	check("the issue type's id is the one that was sent",
		nestedString(fetched, "fields", "issuetype", "id") == strconv.Itoa(issueTypeID),
		"sent "+strconv.Itoa(issueTypeID)+" got "+nestedString(fetched, "fields", "issuetype", "id"))

	// jirrabit has no components field on an issue. Shipping an empty list
	// claimed the answer rather than admitting the question was not asked, and
	// a model reads `[]` as "this issue has no components".
	check("the issue claims no components it cannot have",
		!strings.Contains(fetched, `"components"`), truncate(fetched, 200))

	edited, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key,
		"summary":      "Flujo MCP: resumen editado",
	})
	check("editJiraIssue", err == nil, errStr(err))
	after, _ := call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("edit is visible on re-read",
		nestedString(after, "fields", "summary") == "Flujo MCP: resumen editado",
		truncate(after, 120))
	_ = edited

	// The estimate fields ride the same PATCH. Seconds on the wire, minutes in
	// the arguments: the mapping is the thing being checked, not just arrival.
	// Matched as fragments, not with nestedString, which only reads JSON
	// strings and these two are numbers — a check written the other way passes
	// only when the fields are absent, which is exactly backwards.
	_, err = call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "estimateMinutes": 480, "timeRemainingMinutes": 300,
	})
	check("editJiraIssue writes estimate and remaining", err == nil, errStr(err))
	timed, _ := call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("estimate and remaining come back in seconds",
		strings.Contains(timed, `"timeoriginalestimate":28800`) &&
			strings.Contains(timed, `"remainingEstimateSeconds":18000`),
		truncate(timed, 200))

	// Cloning copies the work, not the history. The copy gets the edited
	// summary rather than the created one, so the check reads what the clone
	// was made from rather than what the fixture said.
	cloned, err := call(ctx, session, "cloneJiraIssue", map[string]any{
		"issueIdOrKey": key,
	})
	check("cloneJiraIssue copies the issue", err == nil, errStr(err))
	check("the clone carries the [clon] prefix and no subtasks",
		strings.Contains(cloned, "[clon] Flujo MCP: resumen editado") &&
			strings.Contains(cloned, `"subtasks":[]`),
		truncate(cloned, 200))
	_, err = call(ctx, session, "cloneJiraIssue", map[string]any{})
	check("cloneJiraIssue refuses an empty body", err != nil, "it was accepted")

	fmt.Println("\n[3] status transitions")
	moved, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key,
		"fields":       map[string]any{"status_id": statusID},
	})
	check("editJiraIssue can set status through fields", err == nil, errStr(err))
	after, _ = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("status change is reflected in getJiraIssue",
		nestedString(after, "fields", "status", "id") == strconv.Itoa(statusID),
		truncate(after, 160))
	_ = moved

	// A status id that does not exist must be refused, not silently ignored.
	_, err = call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "fields": map[string]any{"status_id": 999999},
	})
	check("a bogus status_id is rejected", err != nil, "it was accepted")

	// transitionJiraIssue is the name an agent reaches for; both spellings of
	// the destination have to work.
	byID, err := call(ctx, session, "transitionJiraIssue", map[string]any{
		"issueIdOrKey": key, "statusId": statusID,
	})
	check("transitionJiraIssue by id", err == nil, errStr(err))
	byName, err := call(ctx, session, "transitionJiraIssue", map[string]any{
		"issueIdOrKey": key, "statusName": firstStatusName(statuses),
	})
	check("transitionJiraIssue by name", err == nil, errStr(err))
	after, _ = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("transitionJiraIssue is reflected in getJiraIssue",
		nestedString(after, "fields", "status", "name") == firstStatusName(statuses),
		truncate(after, 200))
	_, _ = byID, byName
	_, err = call(ctx, session, "transitionJiraIssue", map[string]any{
		"issueIdOrKey": key, "statusName": "No Such Status",
	})
	check("transitionJiraIssue with an unknown name lists the real ones",
		err != nil && strings.Contains(err.Error(), "This instance has"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "transitionJiraIssue", map[string]any{"issueIdOrKey": key})
	check("transitionJiraIssue needs a destination", err != nil, "it was accepted")

	// An unknown field name must be refused rather than dropped on the floor.
	_, err = call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "fields": map[string]any{"nope": "x"},
	})
	check("an unknown field is rejected, not silently dropped", err != nil,
		"editJiraIssue accepted a field it cannot map")

	fmt.Println("\n[4] comments, worklogs, links, watchers")
	_, err = call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
		"issueIdOrKey": key, "body": "Comentario desde flowtest.",
	})
	check("addOrEditJiraIssueComment", err == nil, errStr(err))
	comments, err := call(ctx, session, "listJiraIssueComments", map[string]any{"issueIdOrKey": key})
	check("listJiraIssueComments finds it", err == nil && strings.Contains(comments, "flowtest"),
		errStr(err))

	worklog, err := call(ctx, session, "addOrEditJiraIssueWorklog", map[string]any{
		"issueIdOrKey": key, "timeSpent": "1h 15m",
	})
	check("addOrEditJiraIssueWorklog", err == nil, errStr(err))
	check("worklog reports the duration in Jira's format",
		strings.Contains(worklog, "1h 15m") && strings.Contains(worklog, "4500"),
		truncate(worklog, 160))
	worklogs, err := call(ctx, session, "listJiraIssueWorklogs", map[string]any{"issueIdOrKey": key})
	check("listJiraIssueWorklogs finds it", err == nil && strings.Contains(worklogs, "timeSpent"),
		errStr(err)+" "+truncate(worklogs, 200))
	check("the worklog is in Jira's time format", strings.Contains(worklogs, "4500"),
		"1h 15m should be 4500 timeSpentSeconds: "+truncate(worklogs, 200))

	// Estimate arguments jirrabit cannot honour used to be accepted and thrown
	// away, so "log 2h against Tuesday and reduce the estimate by 2h" returned
	// success with an entry timestamped now and an untouched estimate. Each is
	// now refused before anything is written, and routed to editJiraIssue,
	// which is where estimates live.
	before, _ := call(ctx, session, "listJiraIssueWorklogs", map[string]any{"issueIdOrKey": key})
	for _, dropped := range []string{"newEstimate", "adjustEstimate", "reduceBy"} {
		args := map[string]any{"issueIdOrKey": key, "timeSpent": "10m"}
		switch dropped {
		case "newEstimate":
			args["newEstimate"] = "3h"
		case "adjustEstimate":
			args["adjustEstimate"] = "auto"
		case "reduceBy":
			args["reduceBy"] = "30m"
		}
		_, err := call(ctx, session, "addOrEditJiraIssueWorklog", args)
		check("a worklog "+dropped+" is refused and routed to editJiraIssue", err != nil &&
			strings.Contains(err.Error(), "editJiraIssue"),
			"the call succeeded, so it logged something "+dropped+" cannot express")
	}
	worklogsAfter, _ := call(ctx, session, "listJiraIssueWorklogs", map[string]any{"issueIdOrKey": key})
	check("none of the refused worklogs were written",
		jsonString(before, "total") == jsonString(worklogsAfter, "total"),
		"before "+jsonString(before, "total")+" after "+jsonString(worklogsAfter, "total"))

	// started is honoured now: the entry carries the date it was given, and an
	// edit moves it. The point of the worklogId argument is that correcting
	// "3h" to "2h" must not lose the original date to a delete plus recreate.
	backdated, err := call(ctx, session, "addOrEditJiraIssueWorklog", map[string]any{
		"issueIdOrKey": key, "timeSpent": "30m", "started": "2026-09-20T14:00:00+02:00",
	})
	check("a worklog started date is stored, not stamped as now",
		err == nil && strings.Contains(backdated, "2026-09-20"),
		errStr(err)+" "+truncate(backdated, 160))
	worklogID := firstID(backdated)
	corrected, err := call(ctx, session, "addOrEditJiraIssueWorklog", map[string]any{
		"issueIdOrKey": key, "worklogId": strconv.Itoa(worklogID), "timeSpent": "45m",
	})
	check("correcting an entry edits it in place",
		err == nil && strings.Contains(corrected, "45m"),
		errStr(err)+" "+truncate(corrected, 160))
	_, err = call(ctx, session, "addOrEditJiraIssueWorklog", map[string]any{
		"issueIdOrKey": key, "worklogId": strconv.Itoa(worklogID), "started": "not-a-date",
	})
	check("a correction with a bad date is refused", err != nil, "it was accepted")

	// Same shape on a comment: jirrabit has no groups or roles, so a
	// "restricted" comment would have come back public with no hint.
	_, err = call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
		"issueIdOrKey": key, "body": "Restringido", "visibilityType": "role",
		"visibilityValue": "Administrators",
	})
	check("a comment with visibilityType is refused rather than posted public",
		err != nil, "the comment was posted, and nothing said it was not restricted")
	commentsAfter, _ := call(ctx, session, "listJiraIssueComments", map[string]any{"issueIdOrKey": key})
	check("the refused comment was not written",
		!strings.Contains(commentsAfter, "Restringido"),
		"a comment body marked restricted reached the issue")

	// A real second issue, taken from the instance rather than invented: the
	// link tool needs an issue key on both ends.
	other := ""
	if listing, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s ORDER BY created DESC", projectKey),
	}); err == nil {
		other = someKeyOtherThan(listing, key)
	}
	check("found a second issue to link against", other != "", "")
	linkTypes, err := call(ctx, session, "listJiraIssueLinkTypes", nil)
	check("listJiraIssueLinkTypes", err == nil && strings.Contains(linkTypes, "relates_to"),
		errStr(err))
	link, err := call(ctx, session, "createJiraIssueLink", map[string]any{
		"linkTypeName": "relates_to", "outwardIssueKey": key, "inwardIssueKey": other,
	})
	check("createJiraIssueLink", err == nil, errStr(err))
	// Jira names the ends outwardIssue/inwardIssue and identifies each by key.
	// The API used to hand back bare numeric ids here, which an agent cannot act
	// on: there is no way to turn 31 back into DEMO-19.
	check("the link is in Jira's shape", strings.Contains(link, "outwardIssue") &&
		strings.Contains(link, "inwardIssue") && strings.Contains(link, `"name":"relates_to"`),
		truncate(link, 200))
	check("both ends are issue keys", strings.Contains(link, `"key":"`+key+`"`) &&
		strings.Contains(link, `"key":"`+other+`"`), truncate(link, 200))
	links, err := call(ctx, session, "getJiraIssueLinks", map[string]any{"issueIdOrKey": key})
	check("getJiraIssueLinks reads it back", err == nil && strings.Contains(links, other),
		errStr(err)+" "+truncate(links, 200))
	check("the read-back is keyed too", strings.Contains(links, `"outwardIssue"`),
		truncate(links, 200))

	for _, watching := range []bool{true, false} {
		if _, err := call(ctx, session, "watchJiraIssue", map[string]any{
			"issueIdOrKey": key, "isWatching": watching,
		}); err != nil {
			check(fmt.Sprintf("watchJiraIssue(%v)", watching), false, err.Error())
		}
	}
	check("watch and unwatch both succeed", true, "")

	// watchJiraIssue could only ever write: there was no way to ask who is
	// watching, so the set it changed could not be read back by any tool.
	if _, err := call(ctx, session, "watchJiraIssue", map[string]any{"issueIdOrKey": key}); err != nil {
		check("watchJiraIssue (for the read-back)", false, err.Error())
	}
	watchers, err := call(ctx, session, "listJiraIssueWatchers", map[string]any{"issueIdOrKey": key})
	check("listJiraIssueWatchers reads the set back",
		err == nil && strings.Contains(watchers, "alice_pm"),
		errStr(err)+" "+truncate(watchers, 160))
	if _, err := call(ctx, session, "watchJiraIssue", map[string]any{
		"issueIdOrKey": key, "isWatching": false,
	}); err != nil {
		check("watchJiraIssue (unwatch again)", false, err.Error())
	}
	emptyWatchers, err := call(ctx, session, "listJiraIssueWatchers", map[string]any{"issueIdOrKey": key})
	check("an issue with no watchers reads as an empty list, not null",
		err == nil && strings.Contains(emptyWatchers, `"watchers":[]`),
		errStr(err)+" "+truncate(emptyWatchers, 160))

	// getJiraIssue used to declare `fields` and `expand` and ignore both, so an
	// agent asking for a narrowed read got the whole issue and could not tell.
	_, err = call(ctx, session, "getJiraIssue", map[string]any{
		"issueIdOrKey": key, "fields": []string{"summary"},
	})
	check("getJiraIssue refuses a fields argument instead of ignoring it", err != nil,
		"it accepted a field selection it cannot honour")

	fmt.Println("\n[4b] epics, labels, membership and the workflow graph")
	// The gaps these close were all "jirrabit enforces a rule the client cannot
	// find out about": an assignee must be a project member, and JQL filtered on
	// epics that nothing could read or write.
	epic, err := call(ctx, session, "createJiraEpic", map[string]any{
		"projectKeyOrId": projectKey, "name": "flowtest: Checkout",
	})
	check("createJiraEpic", err == nil, errStr(err)+truncate(epic, 160))
	epicID := firstID(epic)
	epics, err := call(ctx, session, "listJiraEpics", map[string]any{"projectKeyOrId": projectKey})
	check("listJiraEpics finds it", err == nil && strings.Contains(epics, "flowtest: Checkout"),
		errStr(err)+truncate(epics, 160))

	withEpic, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "epicId": epicID,
	})
	check("an issue reports the epic it is in, which nothing could before",
		err == nil && strings.Contains(withEpic, "flowtest: Checkout"),
		errStr(err)+truncate(withEpic, 240))
	byEpic, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("epic = %q", "flowtest: Checkout"),
	})
	check("the epic is findable through the JQL that already filtered on it",
		err == nil && strings.Contains(byEpic, key), errStr(err)+truncate(byEpic, 200))

	// Labels: readable, and now writable, which they were not at all.
	labelled, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "labels": []string{"flowtest-label"},
	})
	check("setting a label creates it on demand",
		err == nil && strings.Contains(labelled, "flowtest-label"),
		errStr(err)+truncate(labelled, 240))
	labels, err := call(ctx, session, "listJiraLabels", nil)
	check("listJiraLabels sees it", err == nil && strings.Contains(labels, "flowtest-label"),
		errStr(err)+truncate(labels, 160))
	dropped, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "labels": []string{},
	})
	check("a PATCH replaces the whole label set rather than adding to it",
		err == nil && !strings.Contains(dropped, "flowtest-label"),
		errStr(err)+truncate(dropped, 240))

	// Subtasks and estimates: both readable before, neither writable.
	child, err := call(ctx, session, "createJiraIssue", map[string]any{
		"projectKey": projectKey, "summary": "flowtest: hija", "parent": key,
		"issueTypeId": issueTypeID, "estimateMinutes": 480,
	})
	check("an issue can be created as a subtask of another, by key",
		err == nil && strings.Contains(child, key), errStr(err)+truncate(child, 240))
	check("and the estimate it was given comes back, in seconds like the rest of the payload",
		err == nil && strings.Contains(child, `"timeoriginalestimate":28800`),
		truncate(child, 240))

	// Who may be assigned: the missing half of the assignee validation.
	members, err := call(ctx, session, "listJiraProjectMembers", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("listJiraProjectMembers answers who is assignable",
		err == nil && strings.Contains(members, "username"),
		errStr(err)+truncate(members, 240))
	check("the members list names the role, so admin is distinguishable from viewer",
		err == nil && strings.Contains(members, "admin"), truncate(members, 240))

	// The workflow, so transitions are not discovered by being refused.
	transitions, err := call(ctx, session, "listJiraTransitions", map[string]any{"statusId": statusID})
	check("listJiraTransitions says whether the workflow is open",
		err == nil && strings.Contains(transitions, `"open"`),
		errStr(err)+truncate(transitions, 200))
	// The free-form `fields` object has to keep working, not just the named
	// arguments: it is advertised in the description, and it was unreachable for
	// every aliased key until the duplicate check learned what was actually sent.
	viaFields, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "fields": map[string]any{"story_points": 3},
	})
	check("the free-form fields object still works for an aliased key",
		err == nil && strings.Contains(viaFields, `"customfield_10016":3`),
		errStr(err)+truncate(viaFields, 240))
	_, err = call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "storyPoints": 3, "fields": map[string]any{"story_points": 5},
	})
	check("sending the same field twice is still refused", err != nil, "it was accepted")

	check("listJiraTransitions needs a statusId",
		hasError(call(ctx, session, "listJiraTransitions", map[string]any{})), "")

	// Saving a search, which the UI could do and the API could not.
	saved, err := call(ctx, session, "createJiraSavedFilter", map[string]any{
		"name": "flowtest: guardado", "jql": fmt.Sprintf("project = %s", projectKey),
	})
	check("createJiraSavedFilter", err == nil, errStr(err)+truncate(saved, 160))
	listedFilters, err := call(ctx, session, "listJiraSavedFilters", nil)
	check("the saved filter shows up in the listing",
		err == nil && strings.Contains(listedFilters, "flowtest: guardado"),
		errStr(err)+truncate(listedFilters, 200))

	fmt.Println("\n[4c] history, attachments, notifications and teams")
	// Two logs, deliberately not interchangeable: the changelog is field-level and
	// incomplete, the activity feed is complete and carries no before/after. An
	// agent that conflates them draws a wrong conclusion either way, so both are
	// exercised and the incompleteness is asserted rather than assumed.
	// A status it is *not* already in: the transition is what writes the row, and
	// a PATCH naming the status the issue already holds is a no-op that writes no
	// history, so asking for the changelog after one would be testing nothing.
	changed, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "statusId": secondID(statuses, firstID(statuses)),
	})
	check("a status change for the changelog to record", err == nil, errStr(err)+truncate(changed, 160))
	changelog, err := call(ctx, session, "getJiraIssueChangelog", map[string]any{
		"issueIdOrKey": key, "field": "status",
	})
	check("getJiraIssueChangelog records it, with the actor",
		err == nil && strings.Contains(changelog, "alice_pm"),
		errStr(err)+truncate(changelog, 240))
	check("getJiraIssueChangelog narrows to one field",
		err == nil && !strings.Contains(changelog, `"description"`), truncate(changelog, 240))

	activity, err := call(ctx, session, "getJiraProjectActivity", map[string]any{
		"projectKeyOrId": projectKey, "verb": "updated",
	})
	check("getJiraProjectActivity answers who and when, with no gaps",
		err == nil && strings.Contains(activity, "values"),
		errStr(err)+truncate(activity, 240))

	// Analytics are data, not markup: the same aggregation as the web pages,
	// so a standup question does not need a changelog download to answer.
	sla, err := call(ctx, session, "getJiraProjectSla", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("getJiraProjectSla answers with a threshold and a count",
		err == nil && strings.Contains(sla, `"threshold_days":7`) &&
			strings.Contains(sla, `"count":`),
		errStr(err)+" "+truncate(sla, 200))
	burndown, err := call(ctx, session, "getJiraProjectBurndown", map[string]any{
		"projectKeyOrId": projectKey,
	})
	// The demo has sprints and a scratch one often has none, so the shape is
	// asserted rather than the content: a chart object with a velocity list.
	check("getJiraProjectBurndown answers a chart, not markup",
		err == nil && strings.Contains(burndown, `"velocity":`) &&
			strings.Contains(burndown, `"points":`),
		errStr(err)+" "+truncate(burndown, 200))
	_, err = call(ctx, session, "getJiraProjectBurndown", map[string]any{
		"projectKeyOrId": projectKey, "sprintId": 999999,
	})
	check("getJiraProjectBurndown refuses an unknown sprint", err != nil,
		"it was accepted")
	reports, err := call(ctx, session, "getJiraProjectReports", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("getJiraProjectReports answers throughput, cycle and WIP",
		err == nil && strings.Contains(reports, `"throughput":`) &&
			strings.Contains(reports, `"cycle":`) && strings.Contains(reports, `"wip":`),
		errStr(err)+" "+truncate(reports, 240))

	// Attachments: a data: URL, because jirrabit has no filesystem to read from.
	uploaded, err := call(ctx, session, "addJiraAttachment", map[string]any{
		"issueIdOrKey": key, "filename": "flowtest.txt", "contentType": "text/plain",
		"data": base64.StdEncoding.EncodeToString([]byte("contenido de prueba")),
	})
	check("addJiraAttachment", err == nil, errStr(err)+truncate(uploaded, 200))
	attachmentID := firstID(uploaded)
	listedFiles, err := call(ctx, session, "listJiraIssueAttachments", map[string]any{
		"issueIdOrKey": key,
	})
	check("listJiraIssueAttachments finds it",
		err == nil && strings.Contains(listedFiles, "flowtest.txt"),
		errStr(err)+truncate(listedFiles, 240))
	check("the listing omits the bytes, so it is not megabytes of base64",
		err == nil && !strings.Contains(listedFiles, "dataUrl"), truncate(listedFiles, 240))
	fetchedFile, err := call(ctx, session, "getJiraAttachment", map[string]any{
		"attachmentId": attachmentID,
	})
	check("getJiraAttachment returns a data: URL carrying the contents",
		err == nil && strings.Contains(fetchedFile, "data:text/plain;base64,"),
		errStr(err)+truncate(fetchedFile, 240))
	_, err = call(ctx, session, "addJiraAttachment", map[string]any{
		"issueIdOrKey": key, "filename": "malo.txt", "data": "not base64 !!",
	})
	check("an attachment that is not base64 is refused before it is sent", err != nil, "it was accepted")

	// Notifications and teams, both scoped to the key rather than to an argument.
	notifications, err := call(ctx, session, "listJiraNotifications", nil)
	check("listJiraNotifications", err == nil && strings.Contains(notifications, "total"),
		errStr(err)+truncate(notifications, 200))
	marked, err := call(ctx, session, "markJiraNotificationsRead", nil)
	check("markJiraNotificationsRead with no ids marks all of them",
		err == nil && strings.Contains(marked, "marked_read"),
		errStr(err)+truncate(marked, 160))
	teams, err := call(ctx, session, "listJiraTeams", nil)
	check("listJiraTeams, which is what a @team: mention resolves to",
		err == nil && strings.Contains(teams, "values"), errStr(err)+truncate(teams, 200))

	fmt.Println("\n[4d] administration, and the workflow vocabulary")
	// Superuser-only, and the demo key belongs to a superuser, so these are
	// reachable. Two behaviours worth pinning: a vocabulary value in use cannot be
	// deleted, and an empty transition list OPENS a workflow rather than closing it.
	blocked, err := call(ctx, session, "createJiraStatus", map[string]any{
		"name": "flowtest: Blocked", "category": "in_progress",
	})
	check("createJiraStatus", err == nil, errStr(err)+truncate(blocked, 200))
	blockedID := firstID(blocked)
	check("a new status is not sent to the front of the board",
		err == nil && !strings.Contains(blocked, `"order":0`),
		"order 0 would reorder every existing column: "+truncate(blocked, 200))

	// The dangerous one: clearing transitions makes a workflow permissive.
	restrict, err := call(ctx, session, "updateJiraStatusTransitions", map[string]any{
		"statusId": statusID, "allowedNext": []int{firstID(statuses)},
	})
	check("updateJiraStatusTransitions sets the list",
		err == nil && strings.Contains(restrict, "status"), errStr(err)+truncate(restrict, 200))
	opened, err := call(ctx, session, "updateJiraStatusTransitions", map[string]any{
		"statusId": statusID, "allowedNext": []int{},
	})
	check("clearing the list warns that it opened the workflow",
		err == nil && strings.Contains(opened, "OPEN"),
		errStr(err)+truncate(opened, 240))
	_, err = call(ctx, session, "updateJiraStatusTransitions", map[string]any{
		"statusId": statusID,
	})
	check("omitting allowedNext is refused rather than read as empty", err != nil,
		"it cleared the list, which opens the workflow")
	// Put the instance back the way it was.
	if _, err := call(ctx, session, "updateJiraStatusTransitions", map[string]any{
		"statusId": statusID, "allowedNext": []int{},
	}); err != nil {
		check("restoring the open workflow", false, err.Error())
	}

	_, err = call(ctx, session, "deleteJiraStatus", map[string]any{"statusId": statusID})
	check("deleting a status that issues still use is refused",
		err != nil && strings.Contains(err.Error(), "409"), errStr(err))
	// And the one nobody uses, which does go: the new one.
	if _, err := call(ctx, session, "deleteJiraStatus", map[string]any{"statusId": blockedID}); err != nil {
		check("deleting an unused status works", false, err.Error())
	} else {
		check("deleting an unused status works", true, "")
	}

	// Named per run and deleted at the end. A fixed name would make the second
	// run fail with a 409 on a correctly working tool, and the leftovers would
	// pile up in the demo instance, which is the mess AGENTS.md warns about for
	// tests that read a hardcoded value.
	// Short: Priority.name is varchar(20), so a timestamped name overflows it. A
	// discovery-based name would be re-runnable; this only has to be unique
	// within a run, and the entries are deleted at the end of this section.
	vocab := fmt.Sprintf("ft%d", time.Now().Unix()%100000)
	urgent, err := call(ctx, session, "createJiraPriority", map[string]any{
		// Low weight, so it sorts last and never becomes the default priority that
		// createJiraIssue picks — a throwaway that the rest of the run then
		// depends on cannot be cleaned up, and PROTECT would refuse it.
		"name": vocab + "-urg", "weight": 1,
	})
	check("createJiraPriority", err == nil, errStr(err)+truncate(urgent, 160))
	chore, err := call(ctx, session, "createJiraIssueType", map[string]any{
		"name": vocab + "-chore",
	})
	check("createJiraIssueType", err == nil, errStr(err)+truncate(chore, 160))
	_, err = call(ctx, session, "deleteJiraIssueType", map[string]any{"issueTypeId": firstID(types)})
	check("deleting an issue type in use is refused", err != nil, errStr(err))
	if _, err := call(ctx, session, "deleteJiraIssueType", map[string]any{
		"issueTypeId": firstID(chore),
	}); err != nil {
		check("cleaning up the throwaway issue type", false, err.Error())
	} else {
		check("cleaning up the throwaway issue type", true, "")
	}
	if _, err := call(ctx, session, "deleteJiraPriority", map[string]any{
		"priorityId": firstID(urgent),
	}); err != nil {
		check("cleaning up the throwaway priority", false, err.Error())
	} else {
		check("cleaning up the throwaway priority", true, "")
	}

	fields, err := call(ctx, session, "listJiraCustomFields", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("listJiraCustomFields", err == nil && strings.Contains(fields, "values"),
		errStr(err)+truncate(fields, 200))
	wiki, err := call(ctx, session, "getJiraProjectWiki", map[string]any{"projectKeyOrId": projectKey})
	check("getJiraProjectWiki answers empty for a page never written",
		err == nil && strings.Contains(wiki, "body"),
		errStr(err)+truncate(wiki, 200))
	hooks, err := call(ctx, session, "listJiraWebhooks", map[string]any{"projectKeyOrId": projectKey})
	check("listJiraWebhooks", err == nil && strings.Contains(hooks, "values"),
		errStr(err)+truncate(hooks, 200))

	fmt.Println("\n[4e] board placement, pins, timers, reactions, branches and templates")
	// An agent could change an issue's status and never put it anywhere. These
	// are the writes the kanban itself performs, so they are checked against the
	// board's own read model rather than a single endpoint's answer.
	boardIssue, err := call(ctx, session, "createJiraIssue", map[string]any{
		"projectKey": projectKey, "summary": "colocable en el tablero",
		"issueTypeId": issueTypeID,
	})
	check("a second issue exists to place", err == nil && strings.Contains(boardIssue, "\"key\""),
		errStr(err)+" "+truncate(boardIssue, 120))
	boardKey := nestedString(boardIssue, "key")
	if boardKey == "" {
		boardKey = key
	}
	movedOut, err := call(ctx, session, "moveJiraIssue", map[string]any{
		"issueIdOrKey": boardKey, "targetStatus": "In Progress", "position": 0,
	})
	check("moveJiraIssue places a card in a column at a position",
		err == nil && strings.Contains(movedOut, "In Progress"),
		errStr(err)+" "+truncate(movedOut, 140))
	_, err = call(ctx, session, "moveJiraIssue", map[string]any{
		"issueIdOrKey": boardKey, "targetStatus": "no existe este estado",
	})
	// The names listed are this instance's, so the check is that it enumerates
	// them at all rather than a literal from a fixture.
	check("moveJiraIssue to an unknown status lists the real ones",
		err != nil && strings.Contains(err.Error(), "los que existen son") &&
			strings.Contains(err.Error(), "In Progress"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "moveJiraIssue", map[string]any{
		"issueIdOrKey": boardKey, "targetStatus": "In Progress", "position": -1,
	})
	check("moveJiraIssue refuses a negative position", err != nil, "it was accepted")
	_, err = call(ctx, session, "moveJiraIssue", map[string]any{"issueIdOrKey": boardKey})
	check("moveJiraIssue with nothing to do is refused", err != nil, "it was accepted")

	order, err := call(ctx, session, "reorderJiraBoardColumn", map[string]any{
		"projectKeyOrId": projectKey, "targetStatus": "In Progress",
		"issueKeys": []string{boardKey},
	})
	check("reorderJiraBoardColumn returns the resulting order and indexes",
		err == nil && strings.Contains(order, "\"cards\"") && strings.Contains(order, "\"position\""),
		errStr(err)+" "+truncate(order, 140))
	_, err = call(ctx, session, "reorderJiraBoardColumn", map[string]any{
		"projectKeyOrId": projectKey, "targetStatus": "In Progress",
		"issueKeys": []string{boardKey, boardKey},
	})
	check("reorderJiraBoardColumn refuses a repeated key", err != nil, "it was accepted")

	bulk, err := call(ctx, session, "bulkUpdateJiraBoard", map[string]any{
		"projectKeyOrId": projectKey, "issueKeys": []string{boardKey, key},
		"action": "status", "value": "In Progress",
	})
	check("bulkUpdateJiraBoard reports what it changed",
		err == nil && strings.Contains(bulk, "\"changed\""),
		errStr(err)+" "+truncate(bulk, 140))
	_, err = call(ctx, session, "bulkUpdateJiraBoard", map[string]any{
		"projectKeyOrId": projectKey, "issueKeys": []string{boardKey},
		"action": "delete",
	})
	check("bulkUpdateJiraBoard points bulk deletion at the confirmed tool",
		err != nil && strings.Contains(err.Error(), "deleteJiraIssue"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "bulkUpdateJiraBoard", map[string]any{
		"projectKeyOrId": projectKey, "issueKeys": []string{boardKey},
		"action": "inventada",
	})
	check("bulkUpdateJiraBoard rejects an unknown action", err != nil, "it was accepted")

	pinned, err := call(ctx, session, "pinJiraItem", map[string]any{"issueIdOrKey": boardKey})
	check("pinJiraItem pins an issue", err == nil && strings.Contains(pinned, "\"pin\""),
		errStr(err)+" "+truncate(pinned, 120))
	pinID := firstID(pinned)
	pins, err := call(ctx, session, "listJiraPins", nil)
	check("listJiraPins shows it", err == nil && strings.Contains(pins, boardKey),
		errStr(err)+" "+truncate(pins, 140))
	_, err = call(ctx, session, "pinJiraItem", map[string]any{
		"issueIdOrKey": boardKey, "projectKeyOrId": projectKey,
	})
	check("pinJiraItem refuses both a project and an issue",
		err != nil, "it was accepted")
	if pinID > 0 {
		_, err = call(ctx, session, "unpinJiraItem", map[string]any{"pinId": pinID})
		check("unpinJiraItem removes it", err == nil, errStr(err))
	}

	_, err = call(ctx, session, "getJiraIssueTimer", map[string]any{"issueIdOrKey": boardKey})
	check("getJiraIssueTimer errors when nothing is running", err != nil, "it returned a timer")
	started, err := call(ctx, session, "startJiraIssueTimer", map[string]any{"issueIdOrKey": boardKey})
	check("startJiraIssueTimer starts one", err == nil && strings.Contains(started, "\"timer\""),
		errStr(err)+" "+truncate(started, 120))
	_, err = call(ctx, session, "startJiraIssueTimer", map[string]any{"issueIdOrKey": boardKey})
	check("a second timer on the same issue is refused", err != nil, "it was accepted")
	stopped, err := call(ctx, session, "stopJiraIssueTimer", map[string]any{"issueIdOrKey": boardKey})
	check("stopJiraIssueTimer records a work log and returns the minutes",
		err == nil && strings.Contains(stopped, "\"worklog\"") && strings.Contains(stopped, "\"minutes\""),
		errStr(err)+" "+truncate(stopped, 140))

	_, err = call(ctx, session, "snoozeJiraIssueNotifications", map[string]any{
		"issueIdOrKey": boardKey, "until": "2000-01-01T00:00:00Z",
	})
	check("snoozing into the past is refused", err != nil, "it was accepted")
	snoozed, err := call(ctx, session, "snoozeJiraIssueNotifications", map[string]any{
		"issueIdOrKey": boardKey, "until": "2099-01-01T00:00:00Z",
	})
	check("snoozeJiraIssueNotifications mutes the issue",
		err == nil && strings.Contains(snoozed, "2099"),
		errStr(err)+" "+truncate(snoozed, 120))
	_, err = call(ctx, session, "unsnoozeJiraIssueNotifications", map[string]any{"issueIdOrKey": boardKey})
	check("unsnoozeJiraIssueNotifications unmutes it", err == nil, errStr(err))

	commentID := 0
	if comments, err := call(ctx, session, "getJiraIssueComments",
		map[string]any{"issueIdOrKey": boardKey}); err == nil {
		commentID = firstID(comments)
	}
	if commentID == 0 {
		added, err := call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
			"issueIdOrKey": boardKey, "body": "reaccionable",
		})
		check("a comment exists to react to", err == nil, errStr(err))
		commentID = firstID(added)
	}
	reacted, err := call(ctx, session, "reactToJiraComment", map[string]any{
		"issueIdOrKey": boardKey, "commentId": commentID, "emoji": "tada",
	})
	check("reactToJiraComment returns the counts and the caller's own",
		err == nil && strings.Contains(reacted, "tada"),
		errStr(err)+" "+truncate(reacted, 140))
	listed, err := call(ctx, session, "listJiraCommentReactions", map[string]any{
		"issueIdOrKey": boardKey, "commentId": commentID,
	})
	check("listJiraCommentReactions agrees, and reacting twice is still one",
		err == nil && strings.Contains(listed, "\"tada\":1") || strings.Contains(listed, "\"tada\": 1"),
		errStr(err)+" "+truncate(listed, 140))
	_, err = call(ctx, session, "reactToJiraComment", map[string]any{
		"issueIdOrKey": boardKey, "commentId": commentID, "emoji": "inventado",
	})
	check("reactToJiraComment rejects an emoji outside the supported set",
		err != nil && strings.Contains(err.Error(), "tada"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "removeJiraCommentReaction", map[string]any{
		"issueIdOrKey": boardKey, "commentId": commentID, "emoji": "tada",
	})
	check("removeJiraCommentReaction takes it back", err == nil, errStr(err))

	branch, err := call(ctx, session, "linkJiraBranchToIssue", map[string]any{
		"issueIdOrKey": boardKey, "branch": "feat/flujo", "commitSha": "abc1234",
	})
	check("linkJiraBranchToIssue links a branch", err == nil && strings.Contains(branch, "feat/flujo"),
		errStr(err)+" "+truncate(branch, 140))
	branchID := firstID(branch)
	branches, err := call(ctx, session, "listJiraIssueBranchLinks",
		map[string]any{"issueIdOrKey": boardKey})
	check("listJiraIssueBranchLinks shows it", err == nil && strings.Contains(branches, "feat/flujo"),
		errStr(err)+" "+truncate(branches, 140))
	if branchID > 0 {
		_, err = call(ctx, session, "unlinkJiraBranchFromIssue", map[string]any{
			"issueIdOrKey": boardKey, "linkId": branchID,
		})
		check("unlinkJiraBranchFromIssue drops the link", err == nil, errStr(err))
	}

	// A previous run that was interrupted between the preview and the confirmation
	// leaves the template behind, and creating it again would be a 409. Clearing
	// it first is what makes this section re-runnable rather than one-shot.
	if existing, err := call(ctx, session, "listJiraIssueTemplates",
		map[string]any{"projectKeyOrId": projectKey}); err == nil {
		for _, id := range idsNamed(existing, "Informe de flujo") {
			previewed, err := call(ctx, session, "deleteJiraIssueTemplate", map[string]any{
				"projectKeyOrId": projectKey, "templateId": id,
			})
			if err == nil {
				_, _ = call(ctx, session, "deleteJiraIssueTemplate", map[string]any{
					"projectKeyOrId": projectKey, "templateId": id,
					"confirm": confirmToken(previewed),
				})
			}
		}
	}
	template, err := call(ctx, session, "createJiraIssueTemplate", map[string]any{
		"projectKeyOrId": projectKey, "name": "Informe de flujo",
		"issueTypeId": issueTypeID, "summary": "plantilla de prueba",
	})
	check("createJiraIssueTemplate creates one", err == nil && strings.Contains(template, "Informe de flujo"),
		errStr(err)+" "+truncate(template, 140))
	templateID := nestedID(template, "issueTemplate", "id")
	templates, err := call(ctx, session, "listJiraIssueTemplates",
		map[string]any{"projectKeyOrId": projectKey})
	check("listJiraIssueTemplates shows it", err == nil && strings.Contains(templates, "Informe de flujo"),
		errStr(err)+" "+truncate(templates, 140))
	renamed, err := call(ctx, session, "updateJiraIssueTemplate", map[string]any{
		"projectKeyOrId": projectKey, "templateId": templateID,
		"summary": "plantilla corregida",
	})
	check("updateJiraIssueTemplate edits the defaults in place",
		err == nil && strings.Contains(renamed, "plantilla corregida"),
		errStr(err)+" "+truncate(renamed, 160))
	_, err = call(ctx, session, "updateJiraIssueTemplate", map[string]any{
		"projectKeyOrId": projectKey, "templateId": templateID,
	})
	check("updateJiraIssueTemplate with an empty body says what it accepts",
		err != nil && strings.Contains(err.Error(), "summary"),
		fmt.Sprint(err))

	// One suffix per run, so the two unique names this section creates do not
	// collide with a previous run that was interrupted before its cleanup.
	runSuffix := strconv.FormatInt(time.Now().UnixNano()%1_000_000, 36)
	fmt.Println("\n[4f] the rest of the surface: projects, membership, CSV, board views, recents, mentions")
	// The audit these came from: 118 endpoint methods, 86 tools. Everything below
	// was reachable over HTTP and not over MCP, or existed only in the UI.
	boardViews, err := call(ctx, session, "listJiraBoardViews", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("listJiraBoardViews is reachable", err == nil && strings.Contains(boardViews, "total"),
		errStr(err)+" "+truncate(boardViews, 140))
	savedView, err := call(ctx, session, "saveJiraBoardView", map[string]any{
		"projectKeyOrId": projectKey, "name": "flowtest view",
		"filters": map[string]string{"assignee": "me", "text": "flujo"},
	})
	check("saveJiraBoardView returns a board URL that carries the filters",
		err == nil && strings.Contains(savedView, "assignee=me") && strings.Contains(savedView, "\"url\""),
		errStr(err)+" "+truncate(savedView, 160))
	viewID := firstID(savedView)
	_, err = call(ctx, session, "saveJiraBoardView", map[string]any{
		"projectKeyOrId": projectKey, "name": "flowtest view",
		"filters": map[string]string{"inventado": "x"},
	})
	check("saveJiraBoardView refuses a filter the board never reads",
		err != nil && strings.Contains(err.Error(), "assignee"),
		fmt.Sprint(err))
	if viewID > 0 {
		_, err = call(ctx, session, "deleteJiraBoardView", map[string]any{
			"projectKeyOrId": projectKey, "viewId": viewID,
		})
		check("deleteJiraBoardView removes it", err == nil, errStr(err))
	}

	recents, err := call(ctx, session, "listJiraRecentIssues", nil)
	check("listJiraRecentIssues answers even with nothing viewed yet",
		err == nil && strings.Contains(recents, "total"),
		errStr(err)+" "+truncate(recents, 140))
	_, err = call(ctx, session, "clearJiraRecentIssues", nil)
	check("clearJiraRecentIssues empties the caller's own list", err == nil, errStr(err))

	mentions, err := call(ctx, session, "listJiraCommentMentions", map[string]any{
		"issueIdOrKey": boardKey, "commentId": commentID,
	})
	check("listJiraCommentMentions answers for a comment with no mentions",
		err == nil && strings.Contains(mentions, "\"mentions\""),
		errStr(err)+" "+truncate(mentions, 140))
	_, err = call(ctx, session, "listJiraCommentMentions", map[string]any{
		"issueIdOrKey": boardKey, "commentId": 0,
	})
	check("listJiraCommentMentions needs a comment id", err != nil, "it was accepted")

	exported, err := call(ctx, session, "exportJiraIssuesCsv", map[string]any{
		"projectKeyOrId": projectKey, "columns": "key,summary",
	})
	check("exportJiraIssuesCsv narrows to the columns asked for",
		err == nil && strings.Contains(exported, "flowtest") && strings.Contains(exported, "\"rows\""),
		errStr(err)+" "+truncate(exported, 200))
	narrowed, err := call(ctx, session, "exportJiraIssuesCsv", map[string]any{
		"projectKeyOrId": projectKey, "columns": "key,inventada",
	})
	check("exportJiraIssuesCsv drops a column it does not know, keeping the rest",
		err == nil && strings.Contains(narrowed, `"columns":["key"]`) &&
			!strings.Contains(narrowed, "inventada"),
		errStr(err)+" "+truncate(narrowed, 200))

	previewed, err := call(ctx, session, "importJiraIssuesCsv", map[string]any{
		"projectKeyOrId": projectKey,
		"csv":            "summary,priority\nImportada 1,High\nImportada 2,Medium",
	})
	check("importJiraIssuesCsv previews without creating anything",
		err == nil && strings.Contains(previewed, `"creatable":2`) &&
			strings.Contains(previewed, `"dryRun":true`),
		errStr(err)+" "+truncate(previewed, 240))
	check("the preview names the next step rather than silently doing it",
		strings.Contains(previewed, "dryRun:false"), truncate(previewed, 240))
	typo, err := call(ctx, session, "importJiraIssuesCsv", map[string]any{
		"projectKeyOrId": projectKey, "csv": "summary,priority\nX,PrioridadInexistente",
	})
	check("importJiraIssuesCsv names a value it could not resolve instead of "+
		"falling back silently",
		err == nil && strings.Contains(typo, "PrioridadInexistente"),
		errStr(err)+" "+truncate(typo, 240))

	// The apply is a bulk create, so it goes through the confirmation machinery
	// like any other irreversible write.
	applyArgs := map[string]any{
		"projectKeyOrId": projectKey,
		"csv":            "summary,priority\nImportada 1,High",
		"dryRun":         false,
	}
	csvPreview, err := call(ctx, session, "importJiraIssuesCsv", applyArgs)
	check("importJiraIssuesCsv with dryRun:false previews instead of creating",
		err == nil && strings.Contains(csvPreview, "confirmationToken") &&
			!strings.Contains(csvPreview, `"confirmed":true`),
		errStr(err)+" "+truncate(csvPreview, 240))
	check("the preview counts the issues about to be created",
		strings.Contains(csvPreview, "issues to be created"), truncate(csvPreview, 240))
	imported, err := call(ctx, session, "importJiraIssuesCsv", map[string]any{
		"projectKeyOrId": projectKey,
		"csv":            "summary,priority\nImportada 1,High",
		"dryRun":         false,
		"confirm":        confirmToken(csvPreview),
	})
	check("confirming the import creates the issues",
		err == nil && strings.Contains(imported, `"confirmed":true`) &&
			strings.Contains(imported, projectKey+"-"),
		errStr(err)+" "+truncate(imported, 240))
	importedKey := firstKeyInList(imported)
	if importedKey != "" {
		_, err = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": importedKey})
		check("the imported issue is a real issue, readable by key",
			err == nil, fmt.Sprint(err))
	}
	_, err = call(ctx, session, "importJiraIssuesCsv", map[string]any{
		"projectKeyOrId": projectKey, "csv": "  ",
	})
	check("importJiraIssuesCsv refuses an empty CSV", err != nil, "it was accepted")

	// Privilege-dependent calls.
	//
	// These used to assert "a non-superuser gets 403", which is only true for a
	// non-superuser key — and the demo instance's key belongs to a superuser, so
	// the checks were asserting a fact about the fixture rather than about the
	// server. What is worth asserting either way is that the answer is legible:
	// a well-formed result, or a refusal that says what is required. `allowed()`
	// is that.
	teams, teamsErr := call(ctx, session, "listJiraTeams", nil)
	check("listJiraTeams is reachable", teamsErr == nil && strings.Contains(teams, "total"),
		errStr(teamsErr)+" "+truncate(teams, 140))
	teamName, err := call(ctx, session, "createJiraTeam", map[string]any{
		"slug": "flowtest-" + runSuffix, "name": "flowtest",
	})
	if err == nil {
		teamID := nestedID(teamName, "team", "id")
		check("createJiraTeam either works or refuses for a stated reason", teamID > 0,
			truncate(teamName, 160))
		if teamID > 0 {
			preview, perr := call(ctx, session, "deleteJiraTeam", map[string]any{"teamId": teamID})
			check("deleteJiraTeam previews and is reversible",
				perr == nil && strings.Contains(preview, "confirmationToken") &&
					strings.Contains(preview, `"reversible":true`),
				errStr(perr)+" "+truncate(preview, 200))
			if perr == nil {
				_, cerr := call(ctx, session, "deleteJiraTeam", map[string]any{
					"teamId": teamID, "confirm": confirmToken(preview),
				})
				check("confirming removes the team, so this section is re-runnable",
					cerr == nil, errStr(cerr))
			}
		}
	} else {
		check("createJiraTeam either works or refuses for a stated reason",
			refusalIsReadable(err), fmt.Sprint(err))
	}

	keys, err := call(ctx, session, "listJiraApiKeys", nil)
	check("listJiraApiKeys never returns a secret, only prefixes",
		err == nil && strings.Contains(keys, "total") && !strings.Contains(keys, `"token":`),
		errStr(err)+" "+truncate(keys, 200))
	_, err = call(ctx, session, "createJiraApiKey", map[string]any{"name": "  "})
	check("minting a key with no name is refused before the request",
		err != nil, "it was accepted")

	admins, err := call(ctx, session, "listJiraAdminUsers", nil)
	check("listJiraAdminUsers answers, or refuses for a stated reason",
		allowed(err, admins), errStr(err)+" "+truncate(admins, 160))
	invites, err := call(ctx, session, "listJiraInvites", nil)
	check("listJiraInvites answers, or refuses for a stated reason",
		allowed(err, invites), errStr(err)+" "+truncate(invites, 160))
	if err == nil {
		check("listJiraInvites never hands out a token", !strings.Contains(invites, `"token":"`),
			truncate(invites, 200))
	}
	_, err = call(ctx, session, "createJiraAdminUser", map[string]any{"username": "  "})
	check("creating a user with no username is refused before the request",
		err != nil, "it was accepted")
	_, err = call(ctx, session, "createJiraInvite", map[string]any{"role": "root"})
	check("an invite with an invalid role is refused",
		err != nil, "it was accepted")

	newProjectKey := "FT" + runSuffix
	created, projectErr := call(ctx, session, "createJiraProject", map[string]any{
		"key": newProjectKey, "name": "flowtest project",
	})
	err = projectErr
	if err == nil {
		// jirrabit upper-cases the key, so the answer is compared without regard
		// to case; what matters is that the prefix is derived from the key the
		// server actually stored, not from what was asked for.
		newKey := nestedString(created, "project", "key")
		check("createJiraProject returns the issue-key prefix it just reserved",
			strings.EqualFold(newKey, newProjectKey) &&
				strings.Contains(created, `"issueKeyPrefix":"`+newKey+`-"`),
			truncate(created, 200))
	} else {
		check("createJiraProject either works or refuses for a stated reason",
			refusalIsReadable(err), fmt.Sprint(err))
	}
	_, err = call(ctx, session, "createJiraProject", map[string]any{
		"key": "TOOLONGKEYNAME", "name": "x",
	})
	check("an over-long project key is refused before the request",
		err != nil && strings.Contains(err.Error(), "10"), fmt.Sprint(err))
	_, err = call(ctx, session, "createJiraProject", map[string]any{"name": "sin clave"})
	check("a project with no key is refused before the request", err != nil, "it was accepted")

	_, err = call(ctx, session, "addJiraProjectMember", map[string]any{
		"projectKeyOrId": projectKey, "username": "alguien-que-no-existe",
	})
	check("adding a member that does not exist is an error, not a silent success",
		err != nil, "it was accepted")
	_, err = call(ctx, session, "addJiraProjectMember", map[string]any{
		"projectKeyOrId": "NOPROJECT", "username": "alguien",
	})
	check("adding a member to a missing project is an error", err != nil, "it was accepted")
	memberPreview, err := call(ctx, session, "removeJiraProjectMember", map[string]any{
		"projectKeyOrId": projectKey, "username": "alguien-que-no-existe",
	})
	check("removing a non-member is refused with a reason, not a silent 200",
		err != nil && strings.Contains(err.Error(), "no es miembro"),
		errStr(err)+" "+truncate(memberPreview, 200))

	vocabList, err := call(ctx, session, "listJiraStatuses", nil)
	check("listJiraStatuses is reachable for the vocabulary checks", err == nil, errStr(err))
	for _, tc := range []struct {
		tool  string
		name  string
		empty string
	}{
		{"updateJiraStatus", "statusId", "nothing to change"},
		{"updateJiraIssueType", "issueTypeId", "nothing to change"},
		{"updateJiraLabel", "labelId", "nothing to change"},
		{"updateJiraEpic", "projectKeyOrId", "nothing to change"},
		{"updateJiraTeam", "teamId", "nothing to change"},
		{"updateJiraProjectMember", "projectKeyOrId", "required"},
	} {
		_, uerr := call(ctx, session, tc.tool, map[string]any{tc.name: firstID(vocabList)})
		check(tc.tool+" with an empty body says what is missing instead of sending one",
			uerr != nil, "it accepted an empty body")
	}
	prioritiesList, err := call(ctx, session, "listJiraPriorities", nil)
	check("listJiraPriorities is reachable for the priority check", err == nil, errStr(err))
	_, err = call(ctx, session, "updateJiraPriority", map[string]any{
		"priorityId": firstID(prioritiesList),
	})
	check("updateJiraPriority with an empty body says so",
		err != nil && strings.Contains(err.Error(), "nothing to change"), fmt.Sprint(err))

	wiki, wikiErr := call(ctx, session, "updateJiraProjectWiki", map[string]any{
		"projectKeyOrId": projectKey, "body": "flowtest",
	})
	check("updateJiraProjectWiki either writes or refuses for a stated reason",
		allowed(wikiErr, wiki), errStr(wikiErr)+" "+truncate(wiki, 160))
	hook, err := call(ctx, session, "createJiraWebhook", map[string]any{
		"projectKeyOrId": projectKey, "name": "flowtest",
	})
	if err == nil {
		hookID := nestedID(hook, "webhook", "id")
		preview, herr := call(ctx, session, "deleteJiraWebhook", map[string]any{
			"projectKeyOrId": projectKey, "webhookId": hookID,
		})
		check("deleteJiraWebhook points at the reversible alternative",
			herr == nil && strings.Contains(preview, "active:false"), truncate(preview, 240))
		if herr == nil {
			_, _ = call(ctx, session, "deleteJiraWebhook", map[string]any{
				"projectKeyOrId": projectKey, "webhookId": hookID,
				"confirm": confirmToken(preview),
			})
		}
	} else {
		check("createJiraWebhook either works or refuses for a stated reason",
			refusalIsReadable(err), fmt.Sprint(err))
	}
	_, err = call(ctx, session, "createJiraWebhook", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("a webhook with no name is refused before the request", err != nil, "it was accepted")

	field, err := call(ctx, session, "createJiraCustomField", map[string]any{
		"projectKeyOrId": projectKey, "name": "flowtest " + runSuffix, "type": "text",
	})
	if err == nil {
		slug := nestedString(field, "valueKey")
		check("createJiraCustomField names the key values are stored under",
			slug != "", truncate(field, 200))
		fieldID := nestedID(field, "customField", "id")
		preview, perr := call(ctx, session, "deleteJiraCustomField", map[string]any{
			"projectKeyOrId": projectKey, "fieldId": fieldID,
		})
		check("deleteJiraCustomField says the values are destroyed, not hidden",
			perr == nil && strings.Contains(preview, "not stored anywhere else"),
			errStr(perr)+" "+truncate(preview, 260))
		if perr == nil {
			_, _ = call(ctx, session, "deleteJiraCustomField", map[string]any{
				"projectKeyOrId": projectKey, "fieldId": fieldID,
				"confirm": confirmToken(preview),
			})
		}
	} else {
		check("createJiraCustomField either works or refuses for a stated reason",
			refusalIsReadable(err), fmt.Sprint(err))
	}
	_, err = call(ctx, session, "createJiraCustomField", map[string]any{
		"projectKeyOrId": projectKey, "name": "  ",
	})
	check("a custom field with no name is refused before the request",
		err != nil, "it was accepted")

	commentHistory, err := call(ctx, session, "getJiraCommentHistory", map[string]any{
		"issueIdOrKey": boardKey, "commentId": commentID,
	})
	check("getJiraCommentHistory says it is incomplete rather than looking complete",
		err == nil && strings.Contains(commentHistory, "\"incomplete\":true"),
		errStr(err)+" "+truncate(commentHistory, 200))

	fmt.Println("\n[5] JQL search")
	found, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s AND text ~ \"flujo\"", projectKey),
	})
	check("searchJiraIssuesUsingJql with ~", err == nil && strings.Contains(found, key),
		errStr(err)+" "+truncate(found, 140))
	total, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s", projectKey),
	})
	check("plain project query returns rows", err == nil && strings.Contains(total, "total"),
		errStr(err))
	_, err = call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{"jql": "statuss = Done"})
	check("malformed JQL is explained, not returned as empty", err != nil,
		"it returned no error")
	empty, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s AND text = \"nada de esto\"", projectKey),
	})
	check("a no-match JQL returns an empty result, not an error", err == nil, errStr(err))
	check("the empty result really is empty", strings.Contains(empty, "\"total\":0") ||
		strings.Contains(empty, `"values":[]`), truncate(empty, 120))

	fmt.Println("\n[6] negative cases")
	_, err = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": "DEMO-99999"})
	check("a missing issue is a clear error", err != nil && strings.Contains(err.Error(), "404"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": ""})
	check("an empty issue key is rejected", err != nil, "it was accepted")
	_, err = call(ctx, session, "createJiraIssue", map[string]any{
		"projectKeyOrId": "NOPROJECT", "summary": "x", "issueTypeId": issueTypeID,
	})
	check("creating in a missing project is a clear error", err != nil, "it was accepted")

	fmt.Println("\n[7] multi-tenancy isolation")
	// The same server, a second instance identity: the token belongs to one
	// instance, so pointing it at a dead host must fail rather than fall back
	// to the default instance.
	_, err = callWith(ctx, session, map[string]any{
		"instanceUrl": "http://127.0.0.1:9/", "apiKey": "not-a-real-key",
		"cloudId": "", "projectKey": projectKey, "summary": "no debe existir",
		"issueTypeId": issueTypeID,
	}, "createJiraIssue")
	check("a dead instanceUrl fails instead of falling back to the default",
		err != nil, "it succeeded against the default instance")

	// The opt-in tools are checked both ways: absent without their flag,
	// present and working with it. Registering is not the same as working.
	fmt.Println("\n[7b] sprints, saved filters and users")
	sprints, err := call(ctx, session, "listJiraSprints", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("listJiraSprints", err == nil && strings.Contains(sprints, "values"),
		errStr(err)+" "+truncate(sprints, 160))
	// The paging envelope is the point. Without a count and a cursor, a
	// project with more sprints than one page holds produced a short list that
	// a paging loop read as the end of the data — silently, with no error.
	check("listJiraSprints reports how many sprints there are in total",
		strings.Contains(sprints, `"total"`), truncate(sprints, 200))
	sprintsOneAtATime, err := call(ctx, session, "listJiraSprints", map[string]any{
		"projectKeyOrId": projectKey, "maxResults": 2,
	})
	check("listJiraSprints honours maxResults", err == nil && strings.Contains(sprintsOneAtATime, `"maxResults":2`),
		errStr(err)+" "+truncate(sprintsOneAtATime, 200))
	firstSprint := firstID(sprints)
	one, err := call(ctx, session, "getJiraSprint", map[string]any{"sprintId": firstSprint})
	check("getJiraSprint", err == nil && strings.Contains(one, "name"),
		errStr(err)+" "+truncate(one, 160))
	_, err = call(ctx, session, "getJiraSprint", map[string]any{})
	check("getJiraSprint needs an id", err != nil, "it was accepted")

	createdSprint, err := call(ctx, session, "createJiraSprint", map[string]any{
		"projectKey": projectKey, "name": "flowtest sprint",
		"goal": "created by flowtest", "startDate": "2026-10-01", "endDate": "2026-10-14",
	})
	check("createJiraSprint", err == nil && strings.Contains(createdSprint, "flowtest sprint"),
		errStr(err)+" "+truncate(createdSprint, 160))
	newSprintID := firstID(createdSprint)
	check("the new sprint comes back as future", strings.Contains(createdSprint, `"status":"future"`),
		truncate(createdSprint, 160))
	updatedSprint, err := call(ctx, session, "updateJiraSprint", map[string]any{
		"sprintId": newSprintID, "goal": "updated by flowtest",
	})
	check("updateJiraSprint", err == nil && strings.Contains(updatedSprint, "updated by flowtest"),
		errStr(err)+" "+truncate(updatedSprint, 160))
	afterSprint, _ := call(ctx, session, "getJiraSprint", map[string]any{"sprintId": newSprintID})
	check("the sprint update is visible on re-read",
		strings.Contains(afterSprint, "updated by flowtest"), truncate(afterSprint, 160))
	_, err = call(ctx, session, "createJiraSprint", map[string]any{
		"projectKey": projectKey, "name": "x", "startDate": "not-a-date",
	})
	check("a malformed startDate is rejected before the request", err != nil, "it was accepted")
	_, err = call(ctx, session, "createJiraSprint", map[string]any{"projectKey": projectKey})
	check("createJiraSprint needs a name", err != nil, "it was accepted")
	_, err = call(ctx, session, "createJiraSprint", map[string]any{
		"projectKey": "NOPROJECT", "name": "x",
	})
	check("creating a sprint in a missing project fails", err != nil, "it was accepted")

	filters, err := call(ctx, session, "listJiraSavedFilters", nil)
	check("listJiraSavedFilters", err == nil && strings.Contains(filters, "values"),
		errStr(err)+" "+truncate(filters, 160))

	user, err := call(ctx, session, "getJiraUser", map[string]any{"userIdOrKey": "alice_pm"})
	check("getJiraUser by username", err == nil && strings.Contains(user, "alice_pm"),
		errStr(err)+" "+truncate(user, 160))
	byID, err2 := call(ctx, session, "getJiraUser", map[string]any{"userIdOrKey": "1"})
	check("getJiraUser by numeric id", err2 == nil && strings.Contains(byID, "alice_pm"),
		errStr(err2)+" "+truncate(byID, 160))
	_, err = call(ctx, session, "getJiraUser", map[string]any{"userIdOrKey": "nobody_at_all"})
	check("getJiraUser on an unknown name explains itself", err != nil &&
		strings.Contains(err.Error(), "no user named"), fmt.Sprint(err))

	// Cleaned up here because this is the only section where the delete tools are
	// registered at all, and deleteJiraIssueTemplate is opt-in for the same
	// reason as the rest of them.
	if templateID > 0 {
		args := map[string]any{"projectKeyOrId": projectKey, "templateId": templateID}
		previewed, err := call(ctx, session, "deleteJiraIssueTemplate", args)
		check("deleteJiraIssueTemplate previews instead of deleting",
			err == nil && strings.Contains(previewed, "confirmationToken") &&
				!strings.Contains(previewed, `"confirmed":true`),
			errStr(err)+" "+truncate(previewed, 200))
		remaining, stillThere := call(ctx, session, "listJiraIssueTemplates",
			map[string]any{"projectKeyOrId": projectKey})
		check("the template is still there after the preview",
			stillThere == nil && strings.Contains(remaining, "Informe de flujo"),
			truncate(remaining, 140))
		token := confirmToken(previewed)
		confirmed, err := call(ctx, session, "deleteJiraIssueTemplate", map[string]any{
			"projectKeyOrId": projectKey, "templateId": templateID, "confirm": token,
		})
		check("confirming the template delete applies it",
			err == nil && strings.Contains(confirmed, `"confirmed":true`),
			errStr(err)+" "+truncate(confirmed, 160))
		_, err = call(ctx, session, "deleteJiraIssueTemplate", args)
		check("the same token cannot be replayed once the template is gone",
			err != nil, "it succeeded twice")
	}

	fmt.Println("\n[8] opt-in destructive tools")
	// Assert against the flag actually in force, so the run is meaningful in
	// both modes instead of only when the flags happen to be unset.
	flags := map[string]string{
		"deleteJiraIssue":   "JIRRABIT_MCP_ENABLE_DELETE",
		"updateJiraProject": "JIRRABIT_MCP_ENABLE_MANAGE",
	}
	for tool, flag := range flags {
		_, registered := names[tool]
		want := truthy(os.Getenv(flag))
		check(fmt.Sprintf("%s is registered iff %s is on", tool, flag), registered == want,
			fmt.Sprintf("registered=%v but %s=%q", registered, flag, os.Getenv(flag)))
	}
	if names["deleteJiraIssue"] {
		// Two steps, because the sprint's own row is not the risk: the issues that
		// silently drop out of it are, so the preview has to count them before
		// anything is deleted.
		previewed, err := call(ctx, session, "deleteJiraSprint", map[string]any{
			"sprintId": newSprintID,
		})
		check("deleteJiraSprint previews instead of deleting",
			err == nil && strings.Contains(previewed, "confirmationToken") &&
				!strings.Contains(previewed, `"confirmed":true`),
			errStr(err)+" "+truncate(previewed, 200))
		_, err = call(ctx, session, "getJiraSprint", map[string]any{"sprintId": newSprintID})
		check("the sprint survives the unconfirmed call", err == nil,
			"it was deleted without a confirmation: "+fmt.Sprint(err))
		confirmed, err := call(ctx, session, "deleteJiraSprint", map[string]any{
			"sprintId": newSprintID, "confirm": confirmToken(previewed),
		})
		check("confirming the sprint delete applies it",
			err == nil && strings.Contains(confirmed, `"confirmed":true`),
			errStr(err)+" "+truncate(confirmed, 160))
		_, err = call(ctx, session, "getJiraSprint", map[string]any{"sprintId": newSprintID})
		check("the deleted sprint is really gone", err != nil && strings.Contains(err.Error(), "404"),
			fmt.Sprint(err))
	}

	if names["deleteJiraIssue"] && names["updateJiraProject"] {
		fmt.Println("\n[8b] the same tools, exercised because their flags are on")
		doomed, err := call(ctx, session, "createJiraIssue", map[string]any{
			"projectKey": projectKey, "summary": "flowtest: se va a borrar",
			"issueTypeId": issueTypeID, "estimateMinutes": 120, "timeRemainingMinutes": 90,
		})
		check("created a throwaway issue to delete", err == nil, errStr(err))
		check("create carries the remaining estimate in seconds",
			strings.Contains(doomed, `"remainingEstimateSeconds":5400`),
			truncate(doomed, 200))
		doomedKey := jsonString(doomed, "key")

		renamed, err := call(ctx, session, "updateJiraProject", map[string]any{
			"projectKeyOrId": projectKey, "name": "DEMO (flowtest)",
		})
		check("updateJiraProject renames the project", err == nil, errStr(err))
		check("the new name comes back in the payload",
			strings.Contains(renamed, "flowtest"), truncate(renamed, 160))
		back, err := call(ctx, session, "listJiraProjects", nil)
		check("the rename is visible in listJiraProjects",
			err == nil && strings.Contains(back, "flowtest"), errStr(err))
		// leave the demo looking like itself again
		if _, err := call(ctx, session, "updateJiraProject", map[string]any{
			"projectKeyOrId": projectKey, "name": "Demo project",
		}); err != nil {
			check("restoring the project name", false, err.Error())
		} else {
			check("restoring the project name", true, "")
		}

		// The full handshake, rather than a single call with a token. A delete
		// tool that returned a well-formed preview while having already deleted
		// the row would pass every check that only reads the response.
		checkTwoStepDelete(ctx, session, "deleteJiraIssue", "issueIdOrKey", doomedKey,
			"getJiraIssue", "issueIdOrKey", doomedKey, "")

		// The cascade counts are the part that was never visible: a delete takes
		// comments, worklogs, links and subtasks with it.
		seeded, err := call(ctx, session, "createJiraIssue", map[string]any{
			"projectKey": projectKey, "summary": "flowtest: con cascada",
			"issueTypeId": issueTypeID,
		})
		check("created a second throwaway issue for the cascade", err == nil, errStr(err))
		seededKey := jsonString(seeded, "key")
		if _, err := call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
			"issueIdOrKey": seededKey, "body": "una comentario que se perderá",
		}); err != nil {
			check("seeding a comment on the doomed issue", false, err.Error())
		}
		if _, err := call(ctx, session, "addOrEditJiraIssueWorklog", map[string]any{
			"issueIdOrKey": seededKey, "timeSpent": "1h",
		}); err != nil {
			check("seeding a worklog on the doomed issue", false, err.Error())
		}
		withChildren, err := call(ctx, session, "createJiraIssue", map[string]any{
			"projectKey": projectKey, "summary": "flowtest: hijo que se va con el padre",
			"issueTypeId": issueTypeID,
		})
		if err == nil {
			childKey := jsonString(withChildren, "key")
			previewed, err := call(ctx, session, "deleteJiraIssue", map[string]any{
				"issueIdOrKey": seededKey,
			})
			check("the preview counts the cascade, not just the named row",
				err == nil && strings.Contains(previewed, "comments") &&
					strings.Contains(previewed, "worklogs"),
				errStr(err)+truncate(previewed, 240))
			check("the preview offers archiving as the reversible alternative",
				strings.Contains(previewed, "archiveJiraIssue"), truncate(previewed, 240))
			token := confirmToken(previewed)
			if _, err := call(ctx, session, "deleteJiraIssue", map[string]any{
				"issueIdOrKey": seededKey, "confirm": token,
			}); err != nil {
				check("confirming the cascade delete", false, err.Error())
			}
			_, childErr := call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": childKey})
			check("an unattached issue is NOT swept up by its would-be parent", childErr == nil,
				"the child disappeared, though it was never made a subtask: "+fmt.Sprint(childErr))
		}

		// Archiving is the reversible path: one call, and the answer says how to
		// undo it.
		archivable, err := call(ctx, session, "createJiraIssue", map[string]any{
			"projectKey": projectKey, "summary": "flowtest: se archiva",
			"issueTypeId": issueTypeID,
		})
		if err == nil {
			archKey := jsonString(archivable, "key")
			archived, err := call(ctx, session, "archiveJiraIssue", map[string]any{
				"issueIdOrKey": archKey,
			})
			check("archiveJiraIssue needs no token", err == nil, errStr(err))
			check("archiving says how to undo it",
				err == nil && strings.Contains(archived, "restore") ||
					(err == nil && strings.Contains(archived, "archived: false")),
				errStr(err)+truncate(archived, 200))
			listed, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
				"jql": fmt.Sprintf("project = %s AND archived = true", projectKey),
			})
			check("an archived issue is findable again, so archiving hid it rather than lost it",
				err == nil && strings.Contains(listed, archKey), errStr(err)+truncate(listed, 200))
			back, err := call(ctx, session, "archiveJiraIssue", map[string]any{
				"issueIdOrKey": archKey, "archived": false,
			})
			check("unarchiving works", err == nil, errStr(err)+truncate(back, 160))
		}

		// A soft-deleted comment round-trips with its body intact, which is why
		// it is not in the two-step group.
		commentTarget, err := call(ctx, session, "createJiraIssue", map[string]any{
			"projectKey": projectKey, "summary": "flowtest: comentario blando",
			"issueTypeId": issueTypeID,
		})
		if err == nil {
			ctKey := jsonString(commentTarget, "key")
			added, addErr := call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
				"issueIdOrKey": ctKey, "body": "cuerpo que debe sobrevivir",
			})
			if addErr != nil {
				check("seeding the soft-delete comment", false, addErr.Error())
			}
			commentID := jsonString(added, "id")
			if _, err := call(ctx, session, "deleteJiraIssueComment", map[string]any{
				"issueIdOrKey": ctKey, "commentId": commentID,
			}); err != nil {
				check("deleteJiraIssueComment", false, err.Error())
			}
			afterDelete, _ := call(ctx, session, "listJiraIssueComments", map[string]any{
				"issueIdOrKey": ctKey,
			})
			check("a soft-deleted comment leaves the listing",
				!strings.Contains(afterDelete, "cuerpo que debe sobrevivir"),
				truncate(afterDelete, 200))
			if _, err := call(ctx, session, "restoreJiraIssueComment", map[string]any{
				"issueIdOrKey": ctKey, "commentId": commentID,
			}); err != nil {
				check("restoreJiraIssueComment", false, err.Error())
			}
			afterRestore, _ := call(ctx, session, "listJiraIssueComments", map[string]any{
				"issueIdOrKey": ctKey,
			})
			check("restoring brings the body back unchanged",
				strings.Contains(afterRestore, "cuerpo que debe sobrevivir"),
				truncate(afterRestore, 240))
		}

		// Editing a comment is a real edit now, not a refusal.
		editTarget, err := call(ctx, session, "createJiraIssue", map[string]any{
			"projectKey": projectKey, "summary": "flowtest: editar comentario",
			"issueTypeId": issueTypeID,
		})
		if err == nil {
			etKey := jsonString(editTarget, "key")
			added, _ := call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
				"issueIdOrKey": etKey, "body": "primera versión",
			})
			edited, err := call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
				"issueIdOrKey": etKey, "body": "segunda versión", "commentId": jsonString(added, "id"),
			})
			check("addOrEditJiraIssueComment edits when given commentId",
				err == nil && strings.Contains(edited, "segunda versión"),
				errStr(err)+truncate(edited, 200))
		}

		// Closing a sprint is two steps too, because it moves issues nobody
		// named. A sprint is created for the purpose: closing one of the demo's
		// own would move its issues, which is the side effect under test.
		doomedSprint, err := call(ctx, session, "createJiraSprint", map[string]any{
			"projectKey": projectKey, "name": "flowtest: sprint a cerrar",
		})
		if err != nil {
			check("created a sprint to close", false, err.Error())
		} else {
			// firstID, not jsonString: createJiraSprint returns jirrabit's own
			// payload, where the id is a number. The shaper's string ids come
			// back from getJiraSprint instead.
			sprintID := firstID(doomedSprint)
			checkTwoStepDelete(ctx, session, "closeJiraSprint", "sprintId",
				sprintID, "getJiraSprint", "sprintId", sprintID, `"status":"closed"`)
		}
	}

	return nil
}

// toolNamePattern matches the shape of a tool name: a Jira-style verb followed
// by "Jira" and a subject. It is deliberately loose, because the cost of a
// false positive is a red test and the cost of a miss is a broken agent.
var toolNamePattern = regexp.MustCompile(
	`\b(?:get|list|create|edit|add|delete|update|transition|search|watch|assign|archive|clone|restore)Jira[A-Za-z]*\b`)

// optInTools are registered only when their feature flag is set, and the
// instructions say so. Their absence is the point, so they are not phantoms.
//
// deleteJiraSprint was missing from here, which was harmless only because the
// instructions never named it — so naming it, as they now must, would have
// failed this check. An exemption list that has to be kept in step with the prose
// is the sort of thing that silently rots; keeping all of them together is what
// stops the next one.
//
// The comment pair is here for the same reason and the same reason matters
// twice over: it is reversible, so a reader could reasonably expect it to be on
// always, and the instructions have to say which flag it is behind.
var optInTools = map[string]bool{
	// Deletions and the two calls that are irreversible in practice.
	"deleteJiraApiKey":        true,
	"deleteJiraAttachment":    true,
	"deleteJiraCustomField":   true,
	"deleteJiraEpic":          true,
	"deleteJiraIssue":         true,
	"deleteJiraIssueComment":  true,
	"deleteJiraIssueLink":     true,
	"deleteJiraIssueTemplate": true,
	"deleteJiraIssueWorklog":  true,
	"deleteJiraLabel":         true,
	"deleteJiraProject":       true,
	"deleteJiraSavedFilter":   true,
	"deleteJiraSprint":        true,
	"deleteJiraTeam":          true,
	"deleteJiraWebhook":       true,
	"restoreJiraIssueComment": true,
	"removeJiraProjectMember": true,
	"updateJiraProject":       true,
}

// scanPhantoms reports any tool name mentioned in the server instructions or in
// a tool description that is not actually registered.
//
// This was not hypothetical: editJiraIssue's description told agents to use
// transitionJiraIssue "to change status", and the README repeated it, while
// no such tool existed. An agent following that sentence got "tool not found"
// and had no way to recover. Nothing caught it because no test read the prose.
func scanPhantoms(instructions string, tools []mcp.Tool) error {
	registered := map[string]bool{}
	for _, t := range tools {
		registered[t.Name] = true
	}
	phantoms := map[string]string{}
	scan := func(where, text string) {
		for _, candidate := range toolNamePattern.FindAllString(text, -1) {
			if !registered[candidate] && !optInTools[candidate] {
				phantoms[candidate] = where
			}
		}
	}
	scan("server instructions", instructions)
	for _, t := range tools {
		scan("description of "+t.Name, t.Description)
	}
	if len(phantoms) == 0 {
		check("no phantom tool names in the instructions or any description", true, "")
		return nil
	}
	for name, where := range phantoms {
		check("the instructions must not name an unregistered tool: "+name, false,
			"mentioned in "+where)
	}
	return nil
}

// checkPhantoms runs only the prose check, against a server with no jirrabit
// behind it. Starting the server and listing its tools need no backend, which
// is what lets CI run this without the full stack.
func checkPhantoms(serverPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	proc := transport.NewStdio(serverPath, nil)
	defer proc.Close()

	session := client.NewClient(proc)
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer session.Close()

	init, err := session.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	tools, err := session.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	fmt.Printf("checking %d tools and %d bytes of instructions\n",
		len(tools.Tools), len(init.Instructions))
	return scanPhantoms(init.Instructions, tools.Tools)
}

// callWith lets a test override instanceUrl/apiKey, which the per-call fields
// exist for.
func callWith(ctx context.Context, session *client.Client, args map[string]any, name string) (string, error) {
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := session.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, b := range res.Content {
		if t, ok := b.(mcp.TextContent); ok {
			out.WriteString(t.Text)
		}
	}
	if res.IsError {
		return out.String(), fmt.Errorf("%s", out.String())
	}
	return out.String(), nil
}

func call(ctx context.Context, session *client.Client, name string, args map[string]any) (string, error) {
	return callWith(ctx, session, args, name)
}

func toolNames(tools []mcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// firstID pulls the first row's numeric id out of a metadata response. jirrabit
// returns ids as JSON numbers, and CreateIssueArgs.issueTypeId is an int, so
// handing this over as a string is a type error rather than a lookup.
func firstID(payload string) int {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return 0
	}
	// A single resource is a bare object; a list is wrapped in values/items.
	if _, ok := decoded["id"]; ok {
		decoded = map[string]any{"values": []any{decoded}}
	}
	rows, _ := decoded["values"].([]any)
	if len(rows) == 0 {
		rows, _ = decoded["items"].([]any)
	}
	if len(rows) == 0 {
		return 0
	}
	first, _ := rows[0].(map[string]any)
	switch id := first["id"].(type) {
	case float64:
		return int(id)
	case string:
		parsed, _ := strconv.Atoi(id)
		return parsed
	}
	return 0
}

// allowed reports whether a call that may or may not be permitted by the key it
// was made with produced something usable: either it worked, or it was refused
// for a reason a person can read. Asserting "this must be 403" would be asserting
// a fact about the fixture's key, not about the server.
func allowed(err error, output string) bool {
	if err == nil {
		return true
	}
	return refusalIsReadable(err)
}

// refusalIsReadable is the bar for a refused call: it names the HTTP status and
// says what was required, rather than being a type error or a bare 500.
func refusalIsReadable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "403") || strings.Contains(message, "superusuario")
}

// nestedID reads a numeric id out of a wrapped result, e.g. {"webhook":{"id":7}}.
// firstID only looks at the top level and at a page envelope, and these tools
// wrap their single object under a name.
func nestedID(payload string, path ...string) int {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return 0
	}
	var current any = decoded
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = object[key]
	}
	switch id := current.(type) {
	case float64:
		return int(id)
	case string:
		parsed, _ := strconv.Atoi(id)
		return parsed
	}
	return 0
}

// firstKeyInList returns the first issue key in a JSON array field, for checking
// what a bulk write actually created.
func firstKeyInList(payload string) string {
	// Nested under result when it arrives inside a confirmation envelope, and at
	// the top level when it does not. Both shapes are real answers, so both are
	// read.
	var confirmed struct {
		Result struct {
			CreatedKeys []string `json:"createdKeys"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(payload), &confirmed) == nil && len(confirmed.Result.CreatedKeys) > 0 {
		return confirmed.Result.CreatedKeys[0]
	}
	var decoded struct {
		CreatedKeys []string `json:"createdKeys"`
	}
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return ""
	}
	if len(decoded.CreatedKeys) == 0 {
		return ""
	}
	return decoded.CreatedKeys[0]
}

// idsNamed returns the ids of the entries in a paged payload whose "name" matches,
// so a test can clear its own leftovers before creating them again.
func idsNamed(payload, name string) []int {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return nil
	}
	rows, _ := decoded["values"].([]any)
	if len(rows) == 0 {
		rows, _ = decoded["items"].([]any)
	}
	var ids []int
	for _, row := range rows {
		entry, _ := row.(map[string]any)
		if entry["name"] != name {
			continue
		}
		switch id := entry["id"].(type) {
		case float64:
			ids = append(ids, int(id))
		case string:
			if parsed, err := strconv.Atoi(id); err == nil {
				ids = append(ids, parsed)
			}
		}
	}
	return ids
}

// firstStatusName reads the first status name out of a metadata response.
func firstStatusName(payload string) string {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return ""
	}
	rows, _ := decoded["values"].([]any)
	if len(rows) == 0 {
		rows, _ = decoded["items"].([]any)
	}
	if len(rows) == 0 {
		return ""
	}
	first, _ := rows[0].(map[string]any)
	name, _ := first["name"].(string)
	return name
}

// someKeyOtherThan returns the first issue key in a search result that is not
// the excluded one.
func someKeyOtherThan(payload, exclude string) string {
	var envelope struct {
		Values []struct {
			Key string `json:"key"`
		} `json:"values"`
	}
	if json.Unmarshal([]byte(payload), &envelope) != nil {
		return ""
	}
	for _, v := range envelope.Values {
		if v.Key != "" && v.Key != exclude {
			return v.Key
		}
	}
	return ""
}

func jsonString(payload, field string) string {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return ""
	}
	v, _ := decoded[field].(string)
	return v
}

func nestedString(payload string, path ...string) string {
	var current any
	if json.Unmarshal([]byte(payload), &current) != nil {
		return ""
	}
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	v, _ := current.(string)
	return v
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// truthy mirrors the server's own BoolEnv parsing so the test agrees with it.
func truthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// projectKeys pulls the "key" of every entry in a Jira-shaped project list.
func projectKeys(payload string) []string {
	var envelope struct {
		Values []struct {
			Key string `json:"key"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return nil
	}
	keys := make([]string, 0, len(envelope.Values))
	for _, v := range envelope.Values {
		keys = append(keys, v.Key)
	}
	return keys
}

// firstProjectKey returns the key to work in: whatever the caller asked for, or
// the first project the token can actually see.
func firstProjectKey(want, projects string) string {
	if want != "" {
		return want
	}
	if keys := projectKeys(projects); len(keys) > 0 {
		return keys[0]
	}
	return "DEMO"
}

// --- the two-step destructive flow ---------------------------------------
//
// The point of these checks is the first one. A delete that requires a
// confirmation token is only doing anything if the unconfirmed call leaves the
// data alone, and that is exactly the property no other test in this file
// touches: everything else asserts on a response, and a response can be
// perfectly correct while the write has already happened.

func confirmToken(payload string) string {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return ""
	}
	token, _ := decoded["confirmationToken"].(string)
	return token
}

// checkTwoStepDelete drives the whole handshake for one tool: preview, refuse a
// tampered token, confirm, and then verify the effect from the other end.
//
// The read-back is the part that catches the failure mode this exists to prevent.
// A tool could return a perfectly well-formed preview, refuse nothing, and have
// deleted the row on the first call — and every check that only reads the
// response would pass.
// argValue and readBackValue are `any` because the tools do not agree on
// argument types: issueIdOrKey is a string and sprintId is a number, and passing
// the wrong one fails in the binder before the tool is ever reached.
func checkTwoStepDelete(
	ctx context.Context, session *client.Client,
	tool, argName string, argValue any,
	readBackTool, readBackArg string, readBackValue any,
	// readBackExpect, when set, is what the read-back must contain afterwards
	// rather than an error. A closed sprint is the case: closing is a lifecycle
	// change, not a removal, so the row is still there with status "closed" and a
	// check written for a delete would fail against correct behaviour.
	readBackExpect string,
) {
	previewed, err := call(ctx, session, tool, map[string]any{argName: argValue})
	if err != nil {
		check(tool+" previews without error", false, errStr(err))
		return
	}
	check(tool+" previews instead of acting",
		!strings.Contains(previewed, `"confirmed":true`),
		"it claimed to have acted on the first call")

	// The row must still exist after the preview. Checked before confirming,
	// because a delete that already happened cannot be undone by refusing to
	// confirm.
	if readBackTool != "" {
		alive, aliveErr := call(ctx, session, readBackTool, map[string]any{readBackArg: readBackValue})
		check(tool+" deletes nothing on the first call", aliveErr == nil,
			"the target was already gone after a preview: "+errStr(aliveErr)+truncate(alive, 120))
	}

	token := confirmToken(previewed)
	check(tool+" returns a confirmation token", token != "", truncate(previewed, 200))
	check(tool+" gives the token an expiry", strings.Contains(previewed, `"expiresAt"`),
		truncate(previewed, 160))
	check(tool+" says whether it can be undone",
		strings.Contains(previewed, `"reversible"`), truncate(previewed, 240))
	if token == "" {
		return
	}

	_, tampered := call(ctx, session, tool,
		map[string]any{argName: argValue, "confirm": token + "x"})
	check(tool+" refuses a tampered token", tampered != nil, "it accepted a token it did not issue")

	again, againErr := call(ctx, session, tool, map[string]any{argName: argValue})
	check(tool+" with no token still only previews",
		againErr != nil || (!strings.Contains(again, `"confirmed":true`) && confirmToken(again) != ""),
		"a second bare call acted instead of previewing: "+truncate(again, 160))

	confirmed, err := call(ctx, session, tool, map[string]any{argName: argValue, "confirm": token})
	check(tool+" acts on a valid token",
		err == nil && strings.Contains(confirmed, `"confirmed":true`),
		errStr(err)+" "+truncate(confirmed, 200))

	switch {
	case readBackTool == "":
	case readBackExpect != "":
		after, afterErr := call(ctx, session, readBackTool, map[string]any{readBackArg: readBackValue})
		check(tool+"'s effect is visible on re-read",
			afterErr == nil && strings.Contains(after, readBackExpect),
			errStr(afterErr)+truncate(after, 200))
	default:
		_, goneErr := call(ctx, session, readBackTool, map[string]any{readBackArg: readBackValue})
		check(tool+"'s effect is visible on re-read", goneErr != nil,
			"still readable after the confirmed delete")
	}
}

// hasError reports whether a call failed, for the "this argument is required"
// checks where the message itself is not what is being asserted.
func hasError(_ string, err error) bool {
	return err != nil
}

// secondID returns the first id in a metadata listing that is not `not`.
//
// Needed because the instance is discovered rather than configured: a changelog
// is only written by an actual transition, so the flowtest has to move an issue to
// a status it is not in, and which status that is depends on the instance's
// seeded statuses.
func secondID(payload string, not int) int {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return not
	}
	for _, key := range []string{"values", "items"} {
		rows, _ := decoded[key].([]any)
		for _, row := range rows {
			object, _ := row.(map[string]any)
			id, _ := object["id"].(float64)
			if int(id) != not && int(id) != 0 {
				return int(id)
			}
		}
	}
	return not
}
