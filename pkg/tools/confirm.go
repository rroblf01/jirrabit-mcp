package tools

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
)

// Two-step confirmation for irreversible operations.
//
// The problem it solves is not "an agent might delete something by accident" in
// the abstract. It is that deleteJiraIssue used to be a single call that
// returned success, and the four post_save receivers had already run. There was
// no point at which anybody could see what was about to go: a delete cascades
// to every comment, worklog, attachment and link on the issue, and to every
// subtask, and none of that was ever stated.
//
// So an irreversible call now does nothing on its first invocation. It computes
// what would be lost, says so, and returns a token. The same call with that
// token performs the delete. The token is not a rubber stamp: it is signed over
// the preview, so if the target changed between the two calls the token no
// longer verifies and the agent is told to look again rather than to press on.
//
// What this does not do
//
// It does not obtain the human's consent. Nothing in an MCP server can: the
// tool returns a result and a model decides what to do with it, and a model
// that calls the tool twice in a row without asking anyone has deleted
// something. What the two steps buy is that the *first* step is where the
// consequences are visible, and the server instructions require the second step
// to be taken only after the user has agreed. Together with the operator's
// JIRRABIT_MCP_ENABLE_DELETE flag that is three layers, and the third is a rule
// the model follows rather than a lock. The tool descriptions say so, because
// describing this as a safety guarantee would be the more dangerous claim.

// confirmTTL is how long a token stays usable.
//
// Long enough for a person to read the preview and answer, short enough that a
// token pasted into a transcript next week is not still good. There is no
// single-use enforcement: this server is stateless by design — the pool caches
// clients, not state — and enforcing it would mean a store whose only job is to
// remember which tokens were spent, which is a much larger thing to get right
// than a signed short expiry. The bound that actually matters is that the token
// is bound to a preview digest, so it cannot be reused for a different delete
// or for a target that has since changed.
const confirmTTL = 2 * time.Minute

// confirmPayload is what gets signed. Every field exists to stop a token being
// useful for something other than what it was issued for.
type confirmPayload struct {
	// Op and Target pin the token to one operation on one object. Without them
	// a token issued for a sprint delete would authorise an issue delete.
	Op     string `json:"op"`
	Target string `json:"target"`
	// Instance is the base URL. It is already implied by the signing key, since
	// each instance's secret is derived from its own API key, but carrying it
	// means a token pasted from one deployment into another is rejected with a
	// readable reason rather than a bare signature mismatch.
	Instance string `json:"inst"`
	// Principal is a hash of the API key, so two keys for the same person do not
	// share authority and revoking a key kills its outstanding tokens.
	Principal string `json:"who"`
	// Digest is the hash of the preview the caller was shown. This is the field
	// that makes the token a confirmation rather than a delay.
	Digest  string `json:"digest"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
}

// Preview is what a first call returns instead of acting. The shape is meant to
// be read by a model and relayed to a person, so every count is a real number
// and the irreversibility is stated rather than implied.
type Preview struct {
	// Confirmed is true on the second call, when the operation actually ran.
	Confirmed bool   `json:"confirmed"`
	Operation string `json:"operation"`
	Target    string `json:"target"`
	// What is about to be removed, described for a human.
	Summary string `json:"summary"`
	// What disappears with it, by kind, with real counts. This is the part that
	// was never visible before: the cascade is the actual risk, and a delete that
	// only removes the named row is a lie.
	Cascade []PreviewCascade `json:"cascade,omitempty"`
	// Reversible and Undo say whether this can be taken back, and how. Empty
	// strings mean no, and the tool descriptions point at the alternative.
	Reversible bool   `json:"reversible"`
	Undo       string `json:"undo,omitempty"`
	// Alternative is the safer operation, when one exists for this target.
	Alternative string `json:"alternative,omitempty"`
	// ConfirmationToken goes back on the second call. ExpiresAt is a Unix time so
	// an agent can reason about it without parsing prose.
	ConfirmationToken string `json:"confirmationToken,omitempty"`
	ExpiresAt         int64  `json:"expiresAt,omitempty"`
	// The operation's own result, once it has run.
	Result any `json:"result,omitempty"`
}

// PreviewCascade is one row of "this goes too".
type PreviewCascade struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	// Detail names the affected rows when there are few enough to be worth
	// listing, and is empty when there are many.
	Detail string `json:"detail,omitempty"`
}

// confirmSigner mints and verifies tokens for one instance.
type confirmSigner struct {
	secret    []byte
	principal string
	instance  string
}

func newConfirmSigner(client *jira.Client) confirmSigner {
	return confirmSigner{
		secret:    client.ConfirmSecret(),
		principal: client.ConfirmPrincipal(),
		instance:  client.BaseURL(),
	}
}

// digest hashes a preview. The caller must include everything the user was told,
// because the whole point is that the token is a signature over what was said.
func previewDigest(p Preview) string {
	// The token fields are excluded: including them would make the digest
	// depend on itself.
	p.ConfirmationToken = ""
	p.ExpiresAt = 0
	p.Result = nil
	raw, err := json.Marshal(p)
	if err != nil {
		// Preview is a plain struct of strings, ints and slices; marshalling it
		// cannot fail. A digest of the empty string would still be a stable
		// value, so there is nothing to do but say so loudly.
		panic("jirrabit-mcp: preview digest failed: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s confirmSigner) sign(p confirmPayload) string {
	body, err := json.Marshal(p)
	if err != nil {
		// All fields are scalars, so this cannot fail either.
		panic("jirrabit-mcp: confirm payload marshal failed: " + err.Error())
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// issue issues a token for a preview.
func (s confirmSigner) issue(op, target string, preview Preview) string {
	now := time.Now()
	return s.sign(confirmPayload{
		Op:        op,
		Target:    target,
		Instance:  s.instance,
		Principal: s.principal,
		Digest:    previewDigest(preview),
		Issued:    now.Unix(),
		Expires:   now.Add(confirmTTL).Unix(),
	})
}

// verifyErr explains why a token was refused, or returns nil if it is good.
//
// Every failure names the fix, because a confirmation that says "invalid token"
// and stops leaves the caller with nowhere to go, and the natural wrong guess is
// to ask for a new token — which would re-preview and lose the answer they were
// about to confirm.
func (s confirmSigner) verifyErr(token, op, target string, preview Preview) error {
	parts := splitToken(token)
	if parts == nil {
		return fmt.Errorf(
			"the confirmation token is malformed. Call this tool again without `confirm` to get a fresh one")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf(
			"the confirmation token is malformed. Call this tool again without `confirm` to get a fresh one")
	}
	var payload confirmPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf(
			"the confirmation token is not readable. Call this tool again without `confirm` to get a fresh one")
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(body)
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	// Constant-time: a byte-by-byte compare would leak the signature to an
	// attacker willing to time the call, and there is no reason to offer it.
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return fmt.Errorf(
			"the confirmation token is not valid for this instance and key. It was not issued here, or the API key has " +
				"changed since. Call the tool again without `confirm` to preview afresh")
	}
	if time.Now().Unix() > payload.Expires {
		return fmt.Errorf(
			"the confirmation token expired (%d seconds is the limit, and it was issued for %s). Nothing was changed. "+
				"Call the tool again without `confirm` to preview afresh",
			int(confirmTTL.Seconds()), time.Unix(payload.Issued, 0).Format("15:04:05"))
	}
	if payload.Instance != s.instance {
		return fmt.Errorf(
			"the confirmation token was issued for %s, not %s. Nothing was changed", payload.Instance, s.instance)
	}
	if payload.Principal != s.principal {
		return fmt.Errorf(
			"the confirmation token was issued to a different API key. Nothing was changed")
	}
	if payload.Op != op || payload.Target != target {
		return fmt.Errorf(
			"the confirmation token was issued for %s on %s, not %s on %s. Nothing was changed. "+
				"Call the tool again without `confirm` to preview the operation you actually want",
			payload.Op, payload.Target, op, target)
	}
	if got := previewDigest(preview); got != payload.Digest {
		return fmt.Errorf(
			"%s changed between the preview and this call, so the token no longer matches what you were shown and "+
				"nothing was deleted. Read it again and confirm the new preview", target)
	}
	return nil
}

func splitToken(token string) []string {
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			return []string{token[:i], token[i+1:]}
		}
	}
	return nil
}

// guard is the shape every confirmable tool body follows.
//
// build returns the preview and the work. The work runs only when the token is
// present and verifies, which keeps the destructive path in one place instead of
// duplicated across a dozen handlers — and a handler that forgets the check
// cannot exist, because it is the only way to reach build's second return value.
type confirmable struct {
	signer  confirmSigner
	op      string
	target  string
	build   func() (Preview, func(*jira.Client) (any, error), error)
	noRetry bool
}

// run executes one step of the handshake.
func (c confirmable) run(confirm string, client *jira.Client) (*mcp.CallToolResult, error) {
	preview, work, err := c.build()
	if err != nil {
		return toolError(err)
	}
	if confirm == "" {
		preview.ConfirmationToken = c.signer.issue(c.op, c.target, preview)
		preview.ExpiresAt = time.Now().Add(confirmTTL).Unix()
		return jsonResult(preview)
	}
	if err := c.signer.verifyErr(confirm, c.op, c.target, preview); err != nil {
		// A refusal is a tool error rather than a preview: the caller asked to
		// act and did not, and saying so plainly beats returning a payload that
		// reads like a successful dry run.
		return mcp.NewToolResultError(err.Error()), nil
	}
	result, err := work(client)
	if err != nil {
		return toolError(err)
	}
	preview.Confirmed = true
	preview.ConfirmationToken = ""
	preview.ExpiresAt = 0
	preview.Result = result
	return jsonResult(preview)
}

// unknownOutcome turns a transport failure on a confirmed, irreversible call
// into something honest.
//
// This is the case the retry loop used to get wrong: jirrabit commits the
// delete, the reply is lost, the retry gets a 404, and the agent is told the
// delete failed. The error has to say the outcome is unknown and how to find
// out, because "it failed" and "it worked" send the caller opposite ways.
func unknownOutcome(err error, tool, target, readBack string) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultErrorf(
		"%s on %s did not report a result, so whether it happened is unknown: %v\n\n"+
			"Call %s to check before retrying. Do not retry on the assumption it failed — that is how a completed "+
			"delete gets performed twice, or an agent tells its user something untrue.",
		tool, target, err, readBack,
	), nil
}
