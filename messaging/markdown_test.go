package messaging

import (
	"strings"
	"testing"
)

func TestMiniMarkdown_CodeBlock(t *testing.T) {
	input := "before\n```go\nfmt.Println(\"hello\")\n```\nafter"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "```") {
		t.Errorf("code fences should be stripped: %q", got)
	}
	if !strings.Contains(got, "fmt.Println") {
		t.Errorf("code content should be preserved: %q", got)
	}
}

func TestMiniMarkdown_InlineCode(t *testing.T) {
	input := "use `fmt.Println` to print"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "`") {
		t.Errorf("backticks should be stripped: %q", got)
	}
	if !strings.Contains(got, "fmt.Println") {
		t.Errorf("code content should be preserved: %q", got)
	}
}

func TestMiniMarkdown_Image(t *testing.T) {
	input := "check ![screenshot](https://example.com/img.png) here"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "![") {
		t.Errorf("image syntax should be removed: %q", got)
	}
}

func TestMiniMarkdown_LinkPreserved(t *testing.T) {
	input := "see [docs](https://example.com) for details"
	got := MarkdownToMiniMarkdown(input)
	if !strings.Contains(got, "[docs](https://example.com)") {
		t.Errorf("links should be preserved: %q", got)
	}
}

func TestMiniMarkdown_BoldPreserved(t *testing.T) {
	input := "this is **important** text"
	got := MarkdownToMiniMarkdown(input)
	if !strings.Contains(got, "**important**") {
		t.Errorf("bold should be preserved: %q", got)
	}
}

func TestMiniMarkdown_ItalicPreserved(t *testing.T) {
	input := "this is *emphasized* text"
	got := MarkdownToMiniMarkdown(input)
	if !strings.Contains(got, "*emphasized*") {
		t.Errorf("italic should be preserved: %q", got)
	}
}

func TestMiniMarkdown_BlockquotePreserved(t *testing.T) {
	input := "> this is a quote"
	got := MarkdownToMiniMarkdown(input)
	if !strings.Contains(got, "> this is a quote") {
		t.Errorf("blockquote should be preserved: %q", got)
	}
}

func TestMiniMarkdown_Header(t *testing.T) {
	input := "## Section Title"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "#") {
		t.Errorf("header markers should be converted: %q", got)
	}
	if !strings.Contains(got, "**Section Title**") {
		t.Errorf("header should become bold: %q", got)
	}
}

func TestMiniMarkdown_Table(t *testing.T) {
	input := "| Name | Value |\n|------|-------|\n| foo  | bar   |"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "|") {
		t.Errorf("table pipes should be converted: %q", got)
	}
	if !strings.Contains(got, "foo") || !strings.Contains(got, "bar") {
		t.Errorf("table data should be preserved: %q", got)
	}
}

func TestMiniMarkdown_Strikethrough(t *testing.T) {
	input := "this is ~~deleted~~ text"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "~~") {
		t.Errorf("strikethrough markers should be stripped: %q", got)
	}
	if !strings.Contains(got, "deleted") {
		t.Errorf("strikethrough text should be preserved: %q", got)
	}
}

func TestMiniMarkdown_UnorderedList(t *testing.T) {
	input := "- item one\n- item two\n+ item three"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "- item") || strings.Contains(got, "+ item") {
		t.Errorf("list markers should convert to *: %q", got)
	}
	if !strings.Contains(got, "* item one") {
		t.Errorf("expected '* item one': %q", got)
	}
}

func TestMiniMarkdown_HorizontalRule(t *testing.T) {
	input := "above\n---\nbelow"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "---") {
		t.Errorf("horizontal rule should be removed: %q", got)
	}
	if !strings.Contains(got, "above") || !strings.Contains(got, "below") {
		t.Errorf("surrounding text should be preserved: %q", got)
	}
}

func TestMiniMarkdown_Mixed(t *testing.T) {
	input := "# Title\n\nsome **bold** and *italic* text\n\n```\ncode here\n```\n\n- bullet one\n- bullet two"
	got := MarkdownToMiniMarkdown(input)
	if strings.Contains(got, "```") || strings.Contains(got, "# ") {
		t.Errorf("unsupported markdown artifacts remain: %q", got)
	}
	if !strings.Contains(got, "**Title**") {
		t.Errorf("header should become bold: %q", got)
	}
	if !strings.Contains(got, "**bold**") {
		t.Errorf("bold should be preserved: %q", got)
	}
	if !strings.Contains(got, "*italic*") {
		t.Errorf("italic should be preserved: %q", got)
	}
	if !strings.Contains(got, "* bullet one") {
		t.Errorf("list should use * marker: %q", got)
	}
}
