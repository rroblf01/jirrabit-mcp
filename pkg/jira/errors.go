package jira

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// APIError is a non-2xx response from jirrabit's REST API.
//
// jirrabit is built on django-ninja, which uses a single error envelope:
//
//	{"detail": "message"}                    // HttpError, 404, 400
//	{"detail": [{"loc": [...], "msg": "..."}]} // request validation
//
// So `detail` can be a string or a list of objects; both are flattened to a
// single human-readable sentence. Keeping one shape means the MCP server has a
// single error format to parse regardless of which endpoint failed.
type APIError struct {
	StatusCode int
	Detail     string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	msg := e.Detail
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("jirrabit %s %s: %d %s", e.Method, e.Path, e.StatusCode, msg)
}

// NotFound reports whether the error was a 404. Tool handlers use it to
// return a "no such issue" result rather than a generic failure, because an
// agent asked for a specific key and deserves to be told it does not exist.
func (e *APIError) NotFound() bool { return e.StatusCode == http.StatusNotFound }

// Forbidden reports whether the error was a 403.
func (e *APIError) Forbidden() bool { return e.StatusCode == http.StatusForbidden }

// Unprocessable reports whether the error was a 400 or 422 — jirrabit rejects
// invalid input (a malformed JQL string, an assignee outside the project) with
// these, and the message is worth showing verbatim to the agent.
func (e *APIError) Unprocessable() bool {
	return e.StatusCode == http.StatusBadRequest || e.StatusCode == http.StatusUnprocessableEntity
}

// parseErrorBody turns a response body into a single readable sentence.
func parseErrorBody(body []byte) string {
	var envelope struct {
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Detail) == 0 {
		return strings.TrimSpace(string(body))
	}

	// Most common case: {"detail": "some message"}.
	var message string
	if err := json.Unmarshal(envelope.Detail, &message); err == nil {
		return strings.TrimSpace(message)
	}

	// Validation errors: {"detail": [{"loc": ["body", "jql"], "msg": "..."}]}.
	var problems []struct {
		Loc []any `json:"loc"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(envelope.Detail, &problems); err == nil {
		parts := make([]string, 0, len(problems))
		for _, p := range problems {
			loc := make([]string, 0, len(p.Loc))
			for _, segment := range p.Loc {
				loc = append(loc, fmt.Sprint(segment))
			}
			if len(loc) > 0 {
				parts = append(parts, fmt.Sprintf("%s: %s", strings.Join(loc, "."), p.Msg))
				continue
			}
			parts = append(parts, p.Msg)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}

	// Anything else: show it raw rather than swallowing it.
	return strings.TrimSpace(string(envelope.Detail))
}
