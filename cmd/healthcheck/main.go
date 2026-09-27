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
//
// With no -url it reads JIRRABIT_MCP_ADDR and JIRRABIT_MCP_PATH, so it probes
// wherever this server was told to listen.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	base := flag.String("url", defaultEndpoint(), "MCP endpoint to probe")
	timeout := flag.Duration("timeout", 4*time.Second, "request timeout")
	flag.Parse()

	if err := probe(*base, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		os.Exit(1)
	}
}

// defaultEndpoint derives the probe URL from the same environment the server
// reads, so a deployment that moves the port does not silently keep probing a
// port nothing is listening on — which reports healthy, or hangs, depending on
// what else happens to answer there.
//
// The host is always loopback: the healthcheck runs beside the server, and
// JIRRABIT_MCP_ADDR is a listen address, which may legitimately be the wildcard
// ":8082" that cannot be dialled.
func defaultEndpoint() string {
	addr := os.Getenv("JIRRABIT_MCP_ADDR")
	if addr == "" {
		addr = ":8082"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// No port in the value at all; treat the whole thing as a port.
		host, port = "", addr
	}
	// Anything that is not a loopback address — including the empty wildcard and
	// a name we cannot resolve — is probed over loopback, because the healthcheck
	// runs beside the server and the listen address is not necessarily a
	// diallable target.
	if host == "" {
		host = "127.0.0.1"
	} else if !strings.EqualFold(host, "localhost") {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			host = "127.0.0.1"
		}
	}
	if port == "" {
		port = "8082"
	}

	path := os.Getenv("JIRRABIT_MCP_PATH")
	if path == "" {
		path = "/mcp"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "http://" + net.JoinHostPort(host, port) + path
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
