package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
)

func testSigner() confirmSigner {
	return confirmSigner{
		secret:    []byte("secret-one"),
		principal: "principal-a",
		instance:  "https://one.example.com",
	}
}

func testPreview() Preview {
	return Preview{
		Operation: "deleteJiraIssue",
		Target:    "WEB-7",
		Summary:   `WEB-7 "ship it"`,
		Cascade:   []PreviewCascade{{Kind: "comments", Count: 3}},
	}
}

// A round trip is the whole contract: a token this server issued, for this
// operation, against this preview, verifies.
func TestTokenRoundTrip(t *testing.T) {
	s := testSigner()
	preview := testPreview()
	token := s.issue("deleteJiraIssue", "WEB-7", preview)
	if err := s.verifyErr(token, "deleteJiraIssue", "WEB-7", preview); err != nil {
		t.Fatalf("a fresh token was refused: %v", err)
	}
}

// The preview is bound into the signature. If the counts change between the two
// calls, the token is stale and the operation must not run — otherwise the token
// only delays a delete instead of confirming one.
func TestTokenIsBoundToThePreview(t *testing.T) {
	s := testSigner()
	shown := testPreview()
	token := s.issue("deleteJiraIssue", "WEB-7", shown)

	changed := shown
	changed.Cascade = []PreviewCascade{{Kind: "comments", Count: 4}}
	err := s.verifyErr(token, "deleteJiraIssue", "WEB-7", changed)
	if err == nil {
		t.Fatal("a token verified against a changed preview")
	}
	if !strings.Contains(err.Error(), "changed between the preview") {
		t.Errorf("unhelpful refusal: %v", err)
	}
}

// A token for one thing is not authority over another. This is the case that
// matters most: an agent previews a comment deletion and is handed a token,
// then uses it to delete an issue.
func TestTokenIsBoundToTheOperationAndTarget(t *testing.T) {
	s := testSigner()
	preview := testPreview()

	for _, tc := range []struct {
		op, target string
	}{
		{"deleteJiraProject", "WEB-7"},
		{"deleteJiraIssue", "WEB-8"},
	} {
		token := s.issue("deleteJiraIssue", "WEB-7", preview)
		err := s.verifyErr(token, tc.op, tc.target, preview)
		if err == nil {
			t.Errorf("a token for deleteJiraIssue/WEB-7 verified for %s/%s", tc.op, tc.target)
		}
	}
}

// The signing key is per instance, so a token pasted from one deployment into
// another is rejected. A shared multi-tenant server is exactly where that
// matters: a valid-for-somebody-else token would be a confirmed delete on
// somebody else's data.
func TestTokenFromAnotherInstanceIsRefused(t *testing.T) {
	mine := testSigner()
	theirs := testSigner()
	theirs.secret = []byte("secret-two")
	theirs.instance = "https://two.example.com"

	preview := testPreview()
	token := theirs.issue("deleteJiraIssue", "WEB-7", preview)
	if err := mine.verifyErr(token, "deleteJiraIssue", "WEB-7", preview); err == nil {
		t.Fatal("a token signed for another instance was accepted")
	}
}

// Two API keys for the same person are two credentials, so a token issued to one
// should not authorise the other.
func TestTokenFromAnotherKeyIsRefused(t *testing.T) {
	mine := testSigner()
	theirs := testSigner()
	theirs.principal = "principal-b"

	preview := testPreview()
	token := theirs.issue("deleteJiraIssue", "WEB-7", preview)
	if err := mine.verifyErr(token, "deleteJiraIssue", "WEB-7", preview); err == nil {
		t.Fatal("a token issued to another key was accepted")
	}
}

func TestExpiredTokenIsRefused(t *testing.T) {
	s := testSigner()
	preview := testPreview()
	expired := confirmPayload{
		Op:        "deleteJiraIssue",
		Target:    "WEB-7",
		Instance:  s.instance,
		Principal: s.principal,
		Digest:    previewDigest(preview),
		Issued:    time.Now().Add(-time.Hour).Unix(),
		Expires:   time.Now().Add(-time.Hour).Add(confirmTTL).Unix(),
	}
	err := s.verifyErr(s.sign(expired), "deleteJiraIssue", "WEB-7", preview)
	if err == nil {
		t.Fatal("an expired token was accepted")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("unhelpful refusal: %v", err)
	}
}

// A tampered payload must not verify. The signature covers the body, so editing
// the target inside it breaks the MAC.
func TestTamperedTokenIsRefused(t *testing.T) {
	s := testSigner()
	preview := testPreview()
	token := s.issue("deleteJiraIssue", "WEB-7", preview)
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("token is not two parts: %q", token)
	}
	if err := s.verifyErr(parts[0]+".", "deleteJiraIssue", "WEB-7", preview); err == nil {
		t.Error("a token with no signature was accepted")
	}
	if err := s.verifyErr(parts[0]+"."+parts[1]+"x", "deleteJiraIssue", "WEB-7", preview); err == nil {
		t.Error("a token with an extended signature was accepted")
	}
	if err := s.verifyErr("garbage", "deleteJiraIssue", "WEB-7", preview); err == nil {
		t.Error("a token with no separator was accepted")
	}
	if err := s.verifyErr("!!!.###", "deleteJiraIssue", "WEB-7", preview); err == nil {
		t.Error("a token with invalid base64 was accepted")
	}
}

// Every refusal has to say what to do next. A confirmation that only says
// "invalid" sends the caller to re-preview, which loses the answer they were
// about to agree to.
func TestEveryRefusalSaysWhatToDo(t *testing.T) {
	s := testSigner()
	preview := testPreview()
	cases := map[string]error{
		"garbage": s.verifyErr("garbage", "deleteJiraIssue", "WEB-7", preview),
		"expired": s.verifyErr(s.issue("deleteJiraIssue", "WEB-7", preview), "deleteJiraIssue", "WEB-7", preview),
	}
	for name, err := range cases {
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "without `confirm`") &&
			!strings.Contains(err.Error(), "preview") {
			t.Errorf("%s refusal does not tell the caller what to do: %v", name, err)
		}
	}
}

// The digest must not cover the token it produces, or it would depend on itself.
func TestDigestIgnoresTheTokenFields(t *testing.T) {
	base := testPreview()
	withToken := base
	withToken.ConfirmationToken = "abc"
	withToken.ExpiresAt = 12345
	if previewDigest(base) != previewDigest(withToken) {
		t.Error("the digest depends on the token it produces")
	}
}

// A preview with no cascade rows must be as stable as one with rows: a project
// with no members and a project with three should not hash the same by accident.
func TestDigestDistinguishesCounts(t *testing.T) {
	a := testPreview()
	b := testPreview()
	b.Cascade = []PreviewCascade{{Kind: "comments", Count: 0}}
	if previewDigest(a) == previewDigest(b) {
		t.Error("two different previews hashed the same")
	}
}

// The signer must not be reachable with an empty key: that would accept
// unsigned tokens. Deriving it from a real client is the only path.
func TestConfirmSecretIsDerivedNotTheKeyItself(t *testing.T) {
	c, err := jira.NewClient(jira.Config{
		BaseURL: "https://x.example.com",
		APIKey:  "super-secret-key-value",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	secret := c.ConfirmSecret()
	if len(secret) != 32 {
		t.Fatalf("secret is %d bytes, want 32", len(secret))
	}
	if strings.Contains(string(secret), "super-secret-key-value") {
		t.Error("the secret contains the API key in the clear")
	}
	// Same key, same secret; different key, different secret. The pool keys its
	// cache on the pair, so this is what separates two instances.
	other, _ := jira.NewClient(jira.Config{
		BaseURL: "https://x.example.com",
		APIKey:  "another-key",
	})
	if string(secret) == string(other.ConfirmSecret()) {
		t.Error("two different API keys derived the same secret")
	}
	// The base URL is not in the derivation, so a token is separated by the
	// signature check on Instance rather than by the key. Worth pinning, because
	// it is easy to "fix" by mixing the URL in and thereby break key rotation.
	same, _ := jira.NewClient(jira.Config{
		BaseURL: "https://y.example.com",
		APIKey:  "super-secret-key-value",
	})
	if string(secret) != string(same.ConfirmSecret()) {
		t.Error("the secret depends on the base URL, so the same key on two hosts differs")
	}
}

func TestConfirmPrincipalIsStableAndKeySpecific(t *testing.T) {
	a, _ := jira.NewClient(jira.Config{BaseURL: "https://x", APIKey: "key-one"})
	b, _ := jira.NewClient(jira.Config{BaseURL: "https://x", APIKey: "key-one"})
	c, _ := jira.NewClient(jira.Config{BaseURL: "https://x", APIKey: "key-two"})
	if a.ConfirmPrincipal() != b.ConfirmPrincipal() {
		t.Error("the same key produced two principals")
	}
	if a.ConfirmPrincipal() == c.ConfirmPrincipal() {
		t.Error("two keys produced the same principal")
	}
	if strings.Contains(a.ConfirmPrincipal(), "key-one") {
		t.Error("the principal leaks the key")
	}
}
