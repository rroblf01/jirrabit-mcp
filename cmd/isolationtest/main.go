// Command isolationtest proves that one MCP server can serve several jirrabit
// instances without one caller's credentials ever reaching another's data.
//
// Every other check in this repo asks "does the tool work". This one asks the
// question that matters most for a shared deployment: if the server is handed
// two instances and two keys, can a call end up authenticated as the wrong
// identity, or silently fall back to the server's default instance when a
// per-call instance is named?
//
// Everything runs over a single stdio session and the calls are interleaved
// A, B, A, B. That ordering is the point. A pool that caches a client per
// instance URL, or one that remembers the last key, passes every
// non-interleaved test and fails here.
//
//	go run ./cmd/isolationtest -server ./bin/jirrabit-mcp \
//	  -aURL http://host:8000 -aKey … -aProject WEB \
//	  -bURL http://host:8001 -bKey … -bProject OPS
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

var failures int

func check(name string, ok bool, detail string) {
	mark := "PASS"
	if !ok {
		mark = "FAIL"
		failures++
	}
	line := fmt.Sprintf("  %s  %s", mark, name)
	if !ok && detail != "" {
		line += "  -- " + detail
	}
	fmt.Println(line)
}

func main() {
	serverPath := flag.String("server", "./bin/jirrabit-mcp", "path to the server binary")
	aURL := flag.String("aURL", "", "tenant A instance URL")
	aKey := flag.String("aKey", "", "tenant A api key")
	aProject := flag.String("aProject", "", "tenant A project key, unique to A")
	bURL := flag.String("bURL", "", "tenant B instance URL")
	bKey := flag.String("bKey", "", "tenant B api key")
	bProject := flag.String("bProject", "", "tenant B project key, unique to B")
	timeout := flag.Duration("timeout", 90*time.Second, "overall timeout")
	flag.Parse()

	for name, value := range map[string]string{
		"-aURL": *aURL, "-aKey": *aKey, "-aProject": *aProject,
		"-bURL": *bURL, "-bKey": *bKey, "-bProject": *bProject,
	} {
		if value == "" {
			fmt.Fprintf(os.Stderr, "isolationtest: %s is required\n", name)
			os.Exit(2)
		}
	}
	if err := run(*serverPath, tenant{*aURL, *aKey, *aProject}, tenant{*bURL, *bKey, *bProject}, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "isolationtest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== isolation: %d checks, %d failed ===\n", total, failures)
	if failures > 0 {
		os.Exit(1)
	}
}

var total int

type tenant struct {
	url, key, project string
}

func run(serverPath string, a, b tenant, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	proc := transport.NewStdio(serverPath, nil)
	defer proc.Close()
	session := client.NewClient(proc)
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer session.Close()
	if _, err := session.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// projects asks a named instance, with that instance's key, and returns the
	// project keys the caller can see there.
	projects := func(t tenant) (string, error) {
		return call(ctx, session, "listJiraProjects", map[string]any{
			"instanceUrl": t.url, "apiKey": t.key,
		})
	}

	fmt.Println("each instance answers for itself")
	for _, t := range []tenant{a, b} {
		out, err := projects(t)
		total++
		check(fmt.Sprintf("%s lists %s", t.project, t.project),
			err == nil && strings.Contains(out, t.project), errText(err)+" "+out)
		if err == nil && strings.Contains(out, otherProject(t, a, b)) {
			check(fmt.Sprintf("%s cannot see %s", t.project, otherProject(t, a, b)), false,
				"the other tenant's project appeared: "+out)
		}
	}

	fmt.Println("\na key is rejected by the instance it does not belong to")
	crossed, err := projects(tenant{url: b.url, key: a.key, project: a.project})
	total++
	check("A's key against B is rejected, not served",
		err != nil && !strings.Contains(crossed, b.project),
		fmt.Sprintf("err=%v payload=%s", err, crossed))
	check("the rejection names authentication, not a lookup failure",
		err != nil && strings.Contains(strings.ToLower(err.Error()), "401"),
		fmt.Sprint(err))

	crossed, err = projects(tenant{url: a.url, key: b.key, project: b.project})
	total++
	check("B's key against A is rejected, not served",
		err != nil && !strings.Contains(crossed, a.project),
		fmt.Sprintf("err=%v payload=%s", err, crossed))

	fmt.Println("\na revoked-looking key is rejected outright")
	_, err = projects(tenant{url: a.url, key: a.key + "x", project: a.project})
	total++
	check("a corrupted key never falls back to the default instance", err != nil, fmt.Sprint(err))

	fmt.Println("\ninterleaved calls on one connection stay separated")
	// A, B, A, B, in that order. A client pool keyed on the wrong thing, or a
	// remembered "last key", passes the tests above and fails here.
	for round := 1; round <= 2; round++ {
		for _, t := range []tenant{a, b} {
			out, err := projects(t)
			total++
			check(fmt.Sprintf("round %d: %s still sees only its own", round, t.project),
				err == nil && strings.Contains(out, t.project) &&
					!strings.Contains(out, otherProject(t, a, b)),
				errText(err)+" "+out)
		}
	}

	fmt.Println("\nthe identity is the caller's, not the server's")
	for _, t := range []tenant{a, b} {
		me, err := call(ctx, session, "getJiraCurrentUser", map[string]any{
			"instanceUrl": t.url, "apiKey": t.key,
		})
		total++
		check(fmt.Sprintf("%s resolves to a real user", t.project),
			err == nil && strings.Contains(me, "accountId"), errText(err)+" "+me)
	}
	return nil
}

func otherProject(t, a, b tenant) string {
	if t.project == a.project {
		return b.project
	}
	return a.project
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func call(ctx context.Context, session *client.Client, name string, args map[string]any) (string, error) {
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := session.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, block := range res.Content {
		if text, ok := block.(mcp.TextContent); ok {
			out.WriteString(text.Text)
		}
	}
	if res.IsError {
		return out.String(), fmt.Errorf("%s", out.String())
	}
	return out.String(), nil
}
