package main

import "testing"

// defaultEndpoint reads the environment, so these tests set it rather than
// taking arguments. t.Setenv restores it and forbids t.Parallel, which is the
// right trade for a function whose whole job is reading process state.
func TestDefaultEndpoint(t *testing.T) {
	cases := []struct {
		name string
		addr string
		path string
		want string
		why  string
	}{
		{
			name: "the defaults the server itself uses",
			addr: "",
			path: "",
			want: "http://127.0.0.1:8082/mcp",
			why:  "a bare healthcheck must match an unconfigured server",
		},
		{
			name: "a wildcard listen address",
			addr: ":8082",
			path: "/mcp",
			want: "http://127.0.0.1:8082/mcp",
			why:  "the wildcard cannot be dialled; the healthcheck runs beside the server",
		},
		{
			name: "an explicit loopback port",
			addr: "127.0.0.1:9000",
			path: "/mcp",
			want: "http://127.0.0.1:9000/mcp",
			why:  "moving the port must move the probe with it",
		},
		{
			name: "a wildcard port other than 8082",
			addr: ":9999",
			path: "/mcp",
			want: "http://127.0.0.1:9999/mcp",
			why:  "the port is the part that changes in practice",
		},
		{
			name: "ipv6 loopback",
			addr: "[::1]:8082",
			path: "/mcp",
			want: "http://[::1]:8082/mcp",
			why:  "a v6 listen address still has to be dialled as v6",
		},
		{
			name: "localhost by name",
			addr: "localhost:8082",
			path: "/mcp",
			want: "http://localhost:8082/mcp",
			why:  "the loopback name is already diallable and should be left alone",
		},
		{
			name: "a routable listen address is probed over loopback",
			addr: "203.0.113.10:8082",
			path: "/mcp",
			want: "http://127.0.0.1:8082/mcp",
			why:  "the healthcheck is beside the server, so the public address is the wrong target",
		},
		{
			name: "a custom path",
			addr: ":8082",
			path: "/jira",
			want: "http://127.0.0.1:8082/jira",
			why:  "probing the wrong path on a working server reports unhealthy",
		},
		{
			name: "a path without a leading slash",
			addr: ":8082",
			path: "jira",
			want: "http://127.0.0.1:8082/jira",
			why:  "the server normalises a bare path; so must the probe",
		},
		{
			name: "a bare port with no colon",
			addr: "8082",
			path: "/mcp",
			want: "http://127.0.0.1:8082/mcp",
			why:  "an addr with no host is the wildcard spelled differently",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JIRRABIT_MCP_ADDR", tc.addr)
			t.Setenv("JIRRABIT_MCP_PATH", tc.path)
			if got := defaultEndpoint(); got != tc.want {
				t.Errorf("defaultEndpoint() = %q, want %q — %s", got, tc.want, tc.why)
			}
		})
	}
}
