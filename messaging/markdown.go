package messaging

import (
	"regexp"
	"strings"
)

var (
	reCodeBlock = regexp.MustCompile("(?s)```[^\n]*\n?(.*?)```")
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reImage     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink      = regexp.MustCompile(`\[([^\]]+)\]\(([^)]*)\)`)
	reTableSep  = regexp.MustCompile(`(?m)^\|[\s:|\-]+\|$`)
	reTableRow  = regexp.MustCompile(`(?m)^\|(.+)\|$`)
	reHeader    = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	reBold      = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	reStrike    = regexp.MustCompile(`~~(.+?)~~`)
	reHR        = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
	reUL        = regexp.MustCompile(`(?m)^(\s*)[-+]\s+`)
	reBlankRun  = regexp.MustCompile(`\n{3,}`)
)

// MarkdownToMiniMarkdown converts standard markdown from AI agents into
// RingCentral's Mini-Markdown subset. Supported syntax is preserved;
// unsupported syntax (code blocks, tables, images, etc.) is converted
// to readable plain text.
//
// Mini-Markdown supports: *italic*, **bold**, _underline_, [text](url),
// > quote, * bullet.
func MarkdownToMiniMarkdown(text string) string {
	result := text

	// Code blocks: strip fences, keep content as plain text
	result = reCodeBlock.ReplaceAllStringFunc(result, func(match string) string {
		parts := reCodeBlock.FindStringSubmatch(match)
		if len(parts) > 1 {
			return strings.TrimSpace(parts[1])
		}
		return match
	})

	// Images: remove (extracted and sent separately)
	result = reImage.ReplaceAllString(result, "")

	// Links: keep as-is (Mini-Markdown supports [text](url))

	// Table separator rows: remove
	result = reTableSep.ReplaceAllString(result, "")

	// Table rows: convert to space-delimited plain text
	result = reTableRow.ReplaceAllStringFunc(result, func(match string) string {
		parts := reTableRow.FindStringSubmatch(match)
		if len(parts) > 1 {
			cells := strings.Split(parts[1], "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			return strings.Join(cells, "  ")
		}
		return match
	})

	// Headers: convert to bold (# Title -> **Title**)
	result = reHeader.ReplaceAllString(result, "**$2**")

	// Bold: keep as-is (supported)

	// Strikethrough: strip markers, keep text (not supported)
	result = reStrike.ReplaceAllString(result, "$1")

	// Blockquote: keep as-is (> supported)

	// Horizontal rules: remove
	result = reHR.ReplaceAllString(result, "")

	// Unordered list: convert - and + to * (Mini-Markdown bullet syntax)
	result = reUL.ReplaceAllString(result, "${1}* ")

	// Inline code: strip backticks, keep text (not supported)
	result = reInlineCode.ReplaceAllString(result, "$1")

	// Clean up excessive blank lines
	result = reBlankRun.ReplaceAllString(result, "\n\n")

	return strings.TrimSpace(result)
}
