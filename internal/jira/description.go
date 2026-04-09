package jira

import (
	"fmt"
	"strings"
)

// ExtractDescription converts a Jira description field to plain markdown.
// Jira Cloud uses ADF (Atlassian Document Format, a JSON structure).
// Jira Server/DC uses plain text or wiki markup.
// For v1, we extract text content only — rich formatting is not preserved.
func ExtractDescription(desc any) string {
	if desc == nil {
		return ""
	}

	// Jira Server: description is a plain string.
	if s, ok := desc.(string); ok {
		return s
	}

	// Jira Cloud: description is an ADF JSON object.
	if m, ok := desc.(map[string]any); ok {
		var buf strings.Builder
		extractADFText(m, &buf)
		return strings.TrimSpace(buf.String())
	}

	return fmt.Sprintf("%v", desc)
}

// extractADFText recursively walks an ADF node tree and extracts text content.
func extractADFText(node map[string]any, buf *strings.Builder) {
	// Text nodes have a "text" field.
	if text, ok := node["text"].(string); ok {
		buf.WriteString(text)
		return
	}

	// Hard break nodes emit a newline.
	if nodeType, _ := node["type"].(string); nodeType == "hardBreak" {
		buf.WriteString("\n")
		return
	}

	// Recurse into content array.
	content, ok := node["content"].([]any)
	if !ok {
		return
	}

	nodeType, _ := node["type"].(string)
	for i, child := range content {
		childMap, ok := child.(map[string]any)
		if !ok {
			continue
		}
		extractADFText(childMap, buf)

		// Add paragraph breaks between block-level elements.
		if nodeType == "doc" && i < len(content)-1 {
			buf.WriteString("\n\n")
		}
	}

	// Add newline after block elements within doc content.
	if nodeType == "paragraph" || nodeType == "heading" || nodeType == "bulletList" ||
		nodeType == "orderedList" || nodeType == "listItem" || nodeType == "codeBlock" {
		buf.WriteString("\n")
	}
}
