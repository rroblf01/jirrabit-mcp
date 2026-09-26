package jira

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	for _, page := range []int{1, 2, 17, 1000} {
		token := CursorFromPage(page)
		got, err := DecodeCursor(token)
		if err != nil {
			t.Fatalf("DecodeCursor(CursorFromPage(%d)) returned error: %v", page, err)
		}
		if got != page {
			t.Errorf("round trip of page %d gave %d", page, got)
		}
	}
}

func TestDecodeCursorEmptyMeansFirstPage(t *testing.T) {
	page, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("DecodeCursor(\"\") returned error: %v", err)
	}
	if page != 1 {
		t.Errorf("empty token decoded to page %d, want 1", page)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	// A paging loop that silently reset to page 1 on a bad token would never
	// terminate, so garbage must be an error rather than a default.
	for _, token := range []string{"not-base64!!", base64.RawURLEncoding.EncodeToString([]byte("{oops"))} {
		if _, err := DecodeCursor(token); err == nil {
			t.Errorf("DecodeCursor(%q) accepted a malformed token", token)
		}
	}
}

func TestDecodeCursorAcceptsPaddedBase64(t *testing.T) {
	raw, _ := json.Marshal(cursor{Page: 5, Size: 50})
	if _, err := DecodeCursor(base64.URLEncoding.EncodeToString(raw)); err != nil {
		t.Errorf("padded base64 cursor rejected: %v", err)
	}
}

func TestNextTokenEmptyOnLastPage(t *testing.T) {
	if got := NextToken(Page{}); got != "" {
		t.Errorf("NextToken on a final page = %q, want empty", got)
	}
	last := 7
	if got := NextToken(Page{Next: &last}); got == "" {
		t.Error("NextToken with a next page returned empty")
	}
}

// WithPage must not produce "path&page=1": jirrabit answers that with a 301 to
// "path?page=1", which a client that refuses redirects reports as a failure.
func TestWithPageSeparator(t *testing.T) {
	cases := []struct{ in, want string }{
		{"projects/", "projects/?page=2&size=25"},
		{"projects/DEMO/issues/?status=Open", "projects/DEMO/issues/?status=Open&page=2&size=25"},
	}
	for _, c := range cases {
		if got := WithPage(c.in, 2, 25); got != c.want {
			t.Errorf("WithPage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWithPageClamps(t *testing.T) {
	if got := WithPage("projects/", 0, 0); got != "projects/?page=1&size=50" {
		t.Errorf("WithPage with zero page/size = %q", got)
	}
	if got := WithPage("projects/", 1, 5000); got != "projects/?page=1&size=200" {
		t.Errorf("WithPage with an oversized size = %q", got)
	}
}
