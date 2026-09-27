package main

import "testing"

func TestPubliclyReachable(t *testing.T) {
	cases := []struct {
		name      string
		transport string
		addr      string
		want      bool
		why       string
	}{
		// stdio: the client spawned this process, so there is one caller.
		{"stdio default", "stdio", ":8082", false, "a spawned stdio server has no network listener"},
		{"stdio empty addr", "stdio", "", false, "addr is irrelevant over stdio"},

		// Loopback: same host only, which is what a private server behind a
		// same-host reverse proxy looks like.
		{"http ipv4 loopback", "http", "127.0.0.1:8082", false, "127.0.0.1 is not reachable from another host"},
		{"http ipv4 loopback alt", "http", "127.0.0.53:9000", false, "the whole 127/8 range is loopback"},
		{"http ipv6 loopback", "http", "[::1]:8082", false, "::1 is loopback"},
		{"http localhost", "http", "localhost:8082", false, "the loopback name is not a public interface"},

		// The wildcard and anything bound to a routable address is public.
		{"http wildcard", "http", ":8082", true, "an empty host is every interface, including the public one"},
		{"http wildcard short", "http", ":80", true, "same, with a default-looking port"},
		{"http public ipv4", "http", "203.0.113.10:8082", true, "a routable address is reachable from the internet"},
		{"http private bridge", "http", "172.17.0.2:8082", true, "a container address is reachable by the docker network"},
		{"http public ipv6", "http", "[2001:db8::1]:8082", true, "a routable v6 address"},

		// A hostname in the listen address cannot be resolved here, so the
		// assumption has to be the safe one.
		{"http hostname", "http", "mcp.example.com:8082", true, "an unresolvable listen address is not assumed private"},

		// Unparseable: refuse to assume private.
		{"http unparseable addr", "http", "nonsense", true, "an addr that will not parse is not assumed private"},

		// The transport aliases all have to behave the same, or the guard is
		// bypassable by spelling the transport differently.
		{"streamable-http", "streamable-http", ":8082", true, "alias must not bypass the check"},
		{"streamablehttp", "streamablehttp", ":8082", true, "alias must not bypass the check"},
		{"streamable-http loopback", "streamable-http", "127.0.0.1:8082", false, "loopback is loopback on every alias"},
		{"unknown transport", "carrier-pigeon", ":8082", false, "an unknown transport never gets a listener"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := publiclyReachable(tc.transport, tc.addr); got != tc.want {
				t.Errorf("publiclyReachable(%q, %q) = %v, want %v — %s", tc.transport, tc.addr, got, tc.want, tc.why)
			}
		})
	}
}
