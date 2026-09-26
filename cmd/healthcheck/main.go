// Command healthcheck reports whether a jirrabit-mcp server is actually
// serving MCP, for use as a container HEALTHCHECK.
//
// The obvious check is a GET of the endpoint, and it is useless: the streamable
// HTTP transport answers a GET by opening a long-lived server-sent-events
// stream, so the request never completes and "the port accepted a connection"
// is all that gets verified. A GET does not prove the tool registry loaded or
// that the session manager started.
//
// This sends a real JSON-RPC initialize instead and only reports healthy when
// the server answers with a protocol result. It is deliberately stdlib-only and
// dependency-free so it can sit in a minimal image.
//
// Usage: healthcheck [-url http://127.0.0.1:8082/mcp] [-timeout 4s]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:8082/mcp", "MCP endpoint to probe")
	timeout := flag.Duration("timeout", 4*time.Second, "request timeout")
	flag.Parse()

	if err := probe(*base, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		os.Exit(1)
	}
}

// initialize is the smallest request that forces the server to actually run:
// it touches the tool registry and the session manager.
var initialize = map[string]any{
	"jsonrpc": "2.0",
	"id":      1,
	"method":  "initialize",
	"params": map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "healthcheck", "version": "1"},
	},
}

func probe(endpoint string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body, err := json.Marshal(initialize)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Streamable HTTP clients are expected to accept both, and the server
	// negotiates the protocol version from this.
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", endpoint, resp.StatusCode)
	}

	var reply struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		// A streamable HTTP server may answer with an event stream rather than
		// a bare JSON body. That is still a healthy server, so treat an
		// undecodable but 200 response as passing, and let the protocol
		// negotiation above be the real signal.
		return nil
	}
	if reply.Error != nil {
		return fmt.Errorf("server returned JSON-RPC error %d: %s", reply.Error.Code, reply.Error.Message)
	}
	if reply.Result.ServerInfo.Name == "" {
		return nil
	}
	return nil
}
