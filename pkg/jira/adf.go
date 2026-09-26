package jira

import "strings"

// Atlassian Document Format (ADF) is the JSON rich-text representation that
// Jira Cloud's v3 API uses for descriptions, comments and worklog comments.
//
// jirrabit stores Markdown, and this adapter owns the translation, so tools
// accept and return plain text. That is deliberate: ADF is a Jira-wire concern,
// not something jirrabit's data model should absorb.
//
// A full Markdown-to-ADF converter is a large piece of work with a long tail of
// edge cases, and it is the wrong trade here. Jira agents overwhelmingly send
// and expect prose, and the two conversions below cover the cases that matter:
// block structure, inline code, and preserving nothing dangerous.

// ADFDoc is an ADF document node.
type ADFDoc struct {
	Type    string    `json:"type"`
	Version int       `json:"version"`
	Content []ADFNode `json:"content"`
}

// ADFNode is one node inside an ADF document.
type ADFNode struct {
	Type    string    `json:"type"`
	Attrs   *ADFAttrs `json:"attrs,omitempty"`
	Text    string    `json:"text,omitempty"`
	Content []ADFNode `json:"content,omitempty"`
	Hard    bool      `json:"hardBreak,omitempty"`
}

// ADFAttrs carries node attributes, currently only the language of a code block.
type ADFAttrs struct {
	Language string `json:"language,omitempty"`
}

// ADFText wraps a string in a single-paragraph ADF document. A nil or empty
// string yields nil, because Jira treats a null description and an empty one
// differently and null is the honest answer for "no text".
func ADFText(text string) *ADFDoc {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	paragraphs := strings.Split(trimmed, "\n\n")
	nodes := make([]ADFNode, 0, len(paragraphs))
	for _, block := range paragraphs {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if language, code, isCode := fencedBlock(block); isCode {
			nodes = append(nodes, ADFNode{
				Type:    "codeBlock",
				Attrs:   &ADFAttrs{Language: language},
				Content: []ADFNode{{Type: "text", Text: code}},
			})
			continue
		}
		nodes = append(nodes, ADFNode{
			Type:    "paragraph",
			Content: []ADFNode{{Type: "text", Text: block}},
		})
	}
	if len(nodes) == 0 {
		return nil
	}
	return &ADFDoc{Type: "doc", Version: 1, Content: nodes}
}

// ADFPlainText flattens an ADF document back to plain text.
//
// A nil document is the empty string. Blocks are joined with a blank line, which
// is what Markdown needs to keep a paragraph break — joining with a single
// newline silently merges paragraphs on the round trip.
func ADFPlainText(doc *ADFDoc) string {
	if doc == nil {
		return ""
	}
	blocks := make([]string, 0, len(doc.Content))
	for _, child := range doc.Content {
		if rendered := writeADFNode(child); rendered != "" {
			blocks = append(blocks, rendered)
		}
	}
	return strings.Join(blocks, "\n\n")
}

// writeADFNode renders one node. Block-level nodes are separated by their
// caller with a blank line; inline nodes are concatenated with no separator.
func writeADFNode(node ADFNode) string {
	switch node.Type {
	case "text":
		return node.Text
	case "hardBreak":
		return "\n"
	case "paragraph":
		return strings.TrimRight(joinInline(node.Content), " \t")
	case "heading":
		return "# " + joinInline(node.Content)
	case "bulletList", "orderedList":
		items := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			items = append(items, "- "+joinInline(item.Content))
		}
		return strings.Join(items, "\n")
	case "listItem":
		return joinInline(node.Content)
	case "codeBlock":
		language := "text"
		if node.Attrs != nil && node.Attrs.Language != "" {
			language = node.Attrs.Language
		}
		body := strings.TrimRight(joinInline(node.Content), "\n")
		return "```" + language + "\n" + body + "\n```"
	case "blockquote":
		parts := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			if rendered := writeADFNode(child); rendered != "" {
				parts = append(parts, rendered)
			}
		}
		return strings.Join(parts, "\n\n")
	case "rule":
		return "---"
	default:
		// Unknown node: recurse so nothing is silently dropped.
		return joinInline(node.Content)
	}
}

// joinInline renders inline content with no separator, which is what text runs
// and inline marks need.
func joinInline(nodes []ADFNode) string {
	var out strings.Builder
	for _, child := range nodes {
		out.WriteString(writeADFNode(child))
	}
	return out.String()
}

// fencedBlock recognises a ```lang ... ``` block occupying the whole string.
func fencedBlock(block string) (language, code string, ok bool) {
	if !strings.HasPrefix(block, "```") {
		return "", "", false
	}
	rest := block[3:]
	newline := strings.IndexByte(rest, '\n')
	if newline < 0 {
		return "", "", false
	}
	language = strings.TrimSpace(rest[:newline])
	// Trim the body first so the closing fence sits at the end, then drop the
	// newline that preceded it, so the code text carries no trailing break.
	inner := strings.TrimSpace(rest[newline+1:])
	if !strings.HasSuffix(inner, "```") {
		return "", "", false
	}
	code = strings.TrimRight(strings.TrimSuffix(inner, "```"), "\n")
	if language == "" {
		language = "text"
	}
	return language, code, true
}
