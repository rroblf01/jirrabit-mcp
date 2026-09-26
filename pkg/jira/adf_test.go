package jira

import "testing"

func TestADFTextEmptyIsNil(t *testing.T) {
	// Jira distinguishes a null description from an empty one, and null is the
	// honest answer for "no text".
	for _, in := range []string{"", "   ", "\n\n"} {
		if got := ADFText(in); got != nil {
			t.Errorf("ADFText(%q) = %+v, want nil", in, got)
		}
	}
}

func TestADFTextSingleParagraph(t *testing.T) {
	doc := ADFText("Hello there")
	if doc == nil {
		t.Fatal("ADFText returned nil for real text")
	}
	if doc.Type != "doc" || doc.Version != 1 {
		t.Errorf("doc envelope = %s v%d, want doc v1", doc.Type, doc.Version)
	}
	if len(doc.Content) != 1 || doc.Content[0].Type != "paragraph" {
		t.Fatalf("expected one paragraph, got %+v", doc.Content)
	}
	inner := doc.Content[0].Content
	if len(inner) != 1 || inner[0].Text != "Hello there" {
		t.Errorf("paragraph text = %+v", inner)
	}
}

func TestADFTextBlankLineSeparatesParagraphs(t *testing.T) {
	doc := ADFText("first para\n\nsecond para")
	if len(doc.Content) != 2 {
		t.Fatalf("expected 2 paragraphs, got %d", len(doc.Content))
	}
	if doc.Content[0].Content[0].Text != "first para" {
		t.Errorf("first paragraph = %q", doc.Content[0].Content[0].Text)
	}
	if doc.Content[1].Content[0].Text != "second para" {
		t.Errorf("second paragraph = %q", doc.Content[1].Content[0].Text)
	}
}

func TestADFTextFencedCodeBlock(t *testing.T) {
	doc := ADFText("intro\n\n```go\nfmt.Println(1)\n```")
	if len(doc.Content) != 2 {
		t.Fatalf("expected paragraph + code block, got %d nodes", len(doc.Content))
	}
	block := doc.Content[1]
	if block.Type != "codeBlock" {
		t.Fatalf("second node = %q, want codeBlock", block.Type)
	}
	if block.Attrs == nil || block.Attrs.Language != "go" {
		t.Errorf("code block language = %+v, want go", block.Attrs)
	}
	if block.Content[0].Text != "fmt.Println(1)" {
		t.Errorf("code block body = %q", block.Content[0].Text)
	}
}

func TestADFTextFencedCodeBlockWithoutLanguage(t *testing.T) {
	doc := ADFText("```\nplain\n```")
	if doc.Content[0].Type != "codeBlock" {
		t.Fatalf("node = %q, want codeBlock", doc.Content[0].Type)
	}
	if lang := doc.Content[0].Attrs.Language; lang != "text" {
		t.Errorf("default language = %q, want text", lang)
	}
}

func TestADFPlainTextRoundTrip(t *testing.T) {
	original := "first para\n\nsecond para"
	if got := ADFPlainText(ADFText(original)); got != original {
		t.Errorf("round trip = %q, want %q", got, original)
	}
}

func TestADFPlainTextNil(t *testing.T) {
	if got := ADFPlainText(nil); got != "" {
		t.Errorf("ADFPlainText(nil) = %q, want empty", got)
	}
}

func TestADFPlainTextCodeBlock(t *testing.T) {
	got := ADFPlainText(ADFText("```python\nprint(1)\n```"))
	want := "```python\nprint(1)\n```"
	if got != want {
		t.Errorf("code block round trip = %q, want %q", got, want)
	}
}

// An unknown node type must not silently swallow its text.
func TestADFPlainTextUnknownNodeStillEmitsText(t *testing.T) {
	doc := &ADFDoc{
		Type:    "doc",
		Version: 1,
		Content: []ADFNode{{Type: "somethingNew", Content: []ADFNode{{Type: "text", Text: "kept"}}}},
	}
	if got := ADFPlainText(doc); got != "kept" {
		t.Errorf("unknown node produced %q, want %q", got, "kept")
	}
}
