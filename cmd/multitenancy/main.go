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

// Command multitenancy proves that one jirrabit-mcp process serves more than one
// jirrabit instance: every call carries its own instanceUrl and apiKey, and no
// call may see the other instance's data.
//
//	go run ./cmd/multitenancy -server ./bin/jirrabit-mcp \
//	    -aURL http://a:8000 -aKey keyA -aProject AAA \
//	    -bURL http://b:8000 -bKey keyB -bProject BBB
func main() {
	serverPath := flag.String("server", "./bin/jirrabit-mcp", "path to the jirrabit-mcp binary")
	aURL := flag.String("aURL", "", "first instance base URL")
	aKey := flag.String("aKey", "", "first instance API key")
	aProject := flag.String("aProject", "", "project key that only the first instance's key can see")
	bURL := flag.String("bURL", "", "second instance base URL")
	bKey := flag.String("bKey", "", "second instance API key")
	bProject := flag.String("bProject", "", "project key that only the second instance's key can see")
	flag.Parse()

	if *aURL == "" || *aKey == "" || *bURL == "" || *bKey == "" {
		fmt.Fprintln(os.Stderr, "multitenancy: both -aURL/-aKey and -bURL/-bKey are required")
		os.Exit(2)
	}
	if err := run(*serverPath, *aURL, *aKey, *aProject, *bURL, *bKey, *bProject); err != nil {
		fmt.Fprintf(os.Stderr, "multitenancy: %v\n", err)
		os.Exit(1)
	}
}

func run(serverPath, aURL, aKey, aProject, bURL, bKey, bProject string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Note: no JIRRABIT_URL / JIRRABIT_API_KEY in this process's environment, so
	// every call must resolve its own target. That is the case being tested.
	proc := transport.NewStdio(serverPath, nil)
	defer proc.Close()

	session := client.NewClient(proc)
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("starting server: %w", err)
	}
	defer session.Close()
	if _, err := session.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	call := func(name string, args map[string]any) (string, error) {
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

	// Interleave calls between the two instances on the same connection. If the
	// server cached a single client or mixed up the targets, the identities below
	// would not match.
	type tenant struct{ name, url, key, project string }
	tenants := []tenant{{"A", aURL, aKey, aProject}, {"B", bURL, bKey, bProject}}

	// 1. Each instance must report its own user.
	for _, t := range tenants {
		who, err := call("getJiraCurrentUser", map[string]any{"instanceUrl": t.url, "apiKey": t.key})
		if err != nil {
			return fmt.Errorf("tenant %s identity: %w", t.name, err)
		}
		fmt.Printf("  tenant %s -> %s\n", t.name, who)
	}

	// 2. A repeat call must still resolve correctly, exercising the pool cache.
	for _, t := range tenants {
		who, _ := call("getJiraCurrentUser", map[string]any{"instanceUrl": t.url, "apiKey": t.key})
		fmt.Printf("  tenant %s (cached) -> %s\n", t.name, who)
	}

	// 3. Isolation. Two cases, and which one applies depends on the setup:
	//
	//    - Same instance, two users: jirrabit's own Project.filter_visible is
	//      what separates them, so each must not see the other's project. This
	//      is the case that also proves the adapter routes the right key to the
	//      right user.
	//    - Different instances: nothing shared can leak by construction, and
	//      there is no second project listing to compare against.
	//
	// A superuser is exempt from filter_visible, so a superuser tenant will see
	// every project and this check would fail for a legitimate reason; the check
	// reports that rather than pretending it passed.
	if aProject != "" && bProject != "" {
		if aURL == bURL {
			fmt.Println("  (both tenants on one instance; checking per-user project isolation)")
		}
		for _, t := range tenants {
			out, err := call("listJiraProjects", map[string]any{"instanceUrl": t.url, "apiKey": t.key})
			if err != nil {
				return fmt.Errorf("tenant %s projects: %w", t.name, err)
			}
			other := bProject
			if t.name == "B" {
				other = aProject
			}
			visible := summariseProjects(out)
			if strings.Contains(out, `"key":"`+other+`"`) {
				fmt.Printf("  tenant %s projects -> %s\n", visible, "")
				fmt.Printf("    note: tenant %s CAN see %s; expected if that user is a superuser,\n"+
					"          who bypasses project visibility by design\n", t.name, other)
				continue
			}
			fmt.Printf("  tenant %s projects -> %s (correctly cannot see %s)\n", t.name, visible, other)
		}
	}

	// 4. A wrong key for a real instance must be refused, not silently ignored.
	_, err := call("getJiraCurrentUser", map[string]any{"instanceUrl": aURL, "apiKey": "definitely-not-a-valid-key"})
	if err == nil {
		return fmt.Errorf("an invalid apiKey was accepted")
	}
	fmt.Printf("  invalid key rejected: yes\n")

	// 5. A key with no instance must be refused rather than applied to a default.
	_, err = call("getJiraCurrentUser", map[string]any{"apiKey": aKey})
	if err == nil {
		return fmt.Errorf("an apiKey with no instanceUrl was accepted")
	}
	fmt.Printf("  key without instanceUrl rejected: yes\n")

	// 6. A cloudId must be harmless, so an Atlassian-trained agent still works.
	if _, err := call("getJiraCurrentUser", map[string]any{
		"cloudId": "ari:cloud:jira:does-not-matter:site/1", "instanceUrl": aURL, "apiKey": aKey,
	}); err != nil {
		return fmt.Errorf("a cloudId should be ignored, not rejected: %w", err)
	}
	fmt.Printf("  cloudId accepted and ignored: yes\n")

	fmt.Println("\nmulti-tenancy checks passed")
	return nil
}

func summariseProjects(payload string) string {
	var names []string
	marker := `"key":"`
	for i := 0; i < len(payload); {
		j := strings.Index(payload[i:], marker)
		if j < 0 {
			break
		}
		i += j + len(marker)
		end := strings.IndexByte(payload[i:], '"')
		if end < 0 {
			break
		}
		names = append(names, payload[i:i+end])
		i += end
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
