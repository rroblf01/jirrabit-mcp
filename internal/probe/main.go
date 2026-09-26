// Command probe lists the tools a jirrabit-mcp server exposes, by connecting to
// it as a real MCP client over stdio. It is the quickest way to confirm the
// server starts and registers what it should.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe <path-to-binary>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c := transport.NewStdio(os.Args[1], nil)
	defer c.Close()

	session := client.NewClient(c)
	if err := session.Start(ctx); err != nil {
		fail("start: %v", err)
	}
	defer session.Close()

	if _, err := session.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		fail("initialize: %v", err)
	}

	tools, err := session.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		fail("list tools: %v", err)
	}
	fmt.Printf("%d tools\n", len(tools.Tools))
	for _, t := range tools.Tools {
		fmt.Printf("  %-32s %s\n", t.Name, t.Description)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "probe: "+format+"\n", args...)
	os.Exit(1)
}
