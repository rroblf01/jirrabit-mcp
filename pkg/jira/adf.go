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
// A nil document is the empty string. Inline soft breaks become spaces and hard
// breaks become newlines, so the result is valid Markdown that jirrabit can
// store without further processing.
func ADFPlainText(doc *ADFDoc) string {
	if doc == nil {
		return ""
	}
	var out strings.Builder
	writeADFNode(&out, ADFNode{Type: doc.Type, Content: doc.Content})
	return strings.TrimSpace(out.String())
}

func writeADFNode(out *strings.Builder, node ADFNode) {
	switch node.Type {
	case "text":
		out.WriteString(node.Text)
	case "hardBreak":
		out.WriteString("\n")
	case "paragraph":
		writeADFChildren(out, node)
		out.WriteString("\n")
	case "heading":
		level := 1
		if node.Attrs != nil {
			level = 1
		}
		out.WriteString(strings.Repeat("#", level) + " ")
		writeADFChildren(out, node)
		out.WriteString("\n")
	case "bulletList", "orderedList":
		for _, item := range node.Content {
			out.WriteString("- ")
			writeADFChildren(out, item)
			out.WriteString("\n")
		}
	case "listItem":
		writeADFChildren(out, node)
	case "codeBlock":
		language := ""
		if node.Attrs != nil {
			language = node.Attrs.Language
		}
		if language == "" {
			language = "text"
		}
		out.WriteString("```" + language + "\n")
		writeADFChildren(out, node)
		out.WriteString("\n```\n")
	case "blockquote":
		for _, child := range node.Content {
			writeADFNode(out, child)
		}
	case "rule":
		out.WriteString("---\n")
	default:
		// Unknown node: recurse so nothing is silently dropped.
		writeADFChildren(out, node)
	}
}

func writeADFChildren(out *strings.Builder, node ADFNode) {
	for _, child := range node.Content {
		writeADFNode(out, child)
	}
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
	body := strings.TrimSpace(rest[newline+1:])
	if !strings.HasSuffix(body, "```") {
		return "", "", false
	}
	if language == "" {
		language = "text"
	}
	return language, strings.TrimSuffix(body, "```"), true
}
