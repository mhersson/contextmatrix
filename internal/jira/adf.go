package jira

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
	Content []adfNode      `json:"content"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// ADFToMarkdown converts an Atlassian Document Format document to markdown.
// It never fails: content it cannot render becomes a visible placeholder, so
// nothing is silently dropped from an imported description.
func ADFToMarkdown(doc json.RawMessage) string {
	trimmed := strings.TrimSpace(string(doc))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	var root adfNode
	if err := json.Unmarshal(doc, &root); err != nil {
		return unsupported("invalid document")
	}

	if root.Type != "doc" {
		return strings.TrimSpace(renderBlock(root))
	}

	return strings.TrimSpace(renderBlocks(root.Content, "\n\n"))
}

func unsupported(kind string) string {
	return "[unsupported Jira content: " + kind + "]"
}

func renderBlocks(nodes []adfNode, sep string) string {
	parts := make([]string, 0, len(nodes))

	for _, n := range nodes {
		if s := renderBlock(n); s != "" {
			parts = append(parts, s)
		}
	}

	return strings.Join(parts, sep)
}

func renderBlock(n adfNode) string {
	switch n.Type {
	case "paragraph":
		return renderInline(n.Content)
	case "heading":
		level := min(max(intAttr(n.Attrs, "level", 1), 1), 6)

		return strings.Repeat("#", level) + " " + renderInline(n.Content)
	case "bulletList":
		return renderList(n.Content, func(int) string { return "- " })
	case "orderedList":
		start := intAttr(n.Attrs, "order", 1)

		return renderList(n.Content, func(i int) string { return strconv.Itoa(start+i) + ". " })
	case "codeBlock":
		code := plainText(n.Content)
		fence := codeFence(code)

		return fence + strAttr(n.Attrs, "language") + "\n" + code + "\n" + fence
	case "blockquote":
		return quote(renderBlocks(n.Content, "\n\n"))
	case "rule":
		return "---"
	case "table":
		return renderTable(n)
	case "panel":
		return quote("**" + panelLabel(strAttr(n.Attrs, "panelType")) + "**\n\n" + renderBlocks(n.Content, "\n\n"))
	case "mediaSingle", "mediaGroup":
		return renderBlocks(n.Content, "\n")
	case "media":
		return mediaPlaceholder(n)
	}

	if isInline(n.Type) {
		return renderInline([]adfNode{n})
	}

	// Keep the children of an unknown container so their text survives.
	if body := renderBlocks(n.Content, "\n\n"); body != "" {
		return unsupported(n.Type) + "\n\n" + body
	}

	return unsupported(n.Type)
}

func isInline(typ string) bool {
	switch typ {
	case "text", "hardBreak", "mention", "emoji", "inlineCard", "status", "date", "mediaInline":
		return true
	}

	return false
}

func renderInline(nodes []adfNode) string {
	var b strings.Builder

	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(applyMarks(n.Text, n.Marks))
		case "hardBreak":
			b.WriteString("  \n")
		case "mention":
			b.WriteString("@" + strings.TrimPrefix(strAttr(n.Attrs, "text"), "@"))
		case "emoji":
			if t := strAttr(n.Attrs, "text"); t != "" {
				b.WriteString(t)
			} else {
				b.WriteString(strAttr(n.Attrs, "shortName"))
			}
		case "inlineCard":
			if u := strAttr(n.Attrs, "url"); u != "" {
				b.WriteString("<" + u + ">")
			} else {
				b.WriteString(unsupported(n.Type))
			}
		case "status":
			b.WriteString("[" + strAttr(n.Attrs, "text") + "]")
		case "date":
			b.WriteString(formatDate(strAttr(n.Attrs, "timestamp")))
		case "mediaInline", "media":
			b.WriteString(mediaPlaceholder(n))
		default:
			b.WriteString(unsupported(n.Type))
			b.WriteString(renderInline(n.Content))
		}
	}

	return b.String()
}

// applyMarks wraps the non-space core of text so "bold " renders as
// "**bold** ", which markdown requires.
func applyMarks(text string, marks []adfMark) string {
	core := strings.TrimSpace(text)
	if core == "" {
		return text
	}

	// core is a substring of text, so the index is found; the guard only
	// keeps a future change from slicing with -1.
	start := strings.Index(text, core)
	if start < 0 {
		return text
	}

	lead := text[:start]
	trail := text[start+len(core):]

	var (
		code, strong, em, strike bool
		href                     string
	)

	for _, m := range marks {
		switch m.Type {
		case "code":
			code = true
		case "strong":
			strong = true
		case "em":
			em = true
		case "strike":
			strike = true
		case "link":
			href = strAttr(m.Attrs, "href")
		}
	}

	if code {
		if strings.Contains(core, "`") {
			core = "`` " + core + " ``"
		} else {
			core = "`" + core + "`"
		}
	}

	if strong {
		core = "**" + core + "**"
	}

	if em {
		core = "_" + core + "_"
	}

	if strike {
		core = "~~" + core + "~~"
	}

	if href != "" {
		core = "[" + core + "](" + href + ")"
	}

	return lead + core + trail
}

func renderList(items []adfNode, marker func(int) string) string {
	var lines []string

	for i, it := range items {
		m := marker(i)
		pad := strings.Repeat(" ", len(m))

		body := renderBlock(it)
		if it.Type == "listItem" {
			body = renderBlocks(it.Content, "\n")
		}

		for j, line := range strings.Split(body, "\n") {
			switch {
			case j == 0:
				lines = append(lines, m+line)
			case line == "":
				lines = append(lines, "")
			default:
				lines = append(lines, pad+line)
			}
		}
	}

	return strings.Join(lines, "\n")
}

func renderTable(n adfNode) string {
	var rows [][]string

	width := 0

	for _, row := range n.Content {
		if row.Type != "tableRow" {
			continue
		}

		cells := make([]string, 0, len(row.Content))
		for _, cell := range row.Content {
			cells = append(cells, tableCell(cell))
		}

		width = max(width, len(cells))
		rows = append(rows, cells)
	}

	if width == 0 {
		return ""
	}

	var b strings.Builder

	for i, cells := range rows {
		for len(cells) < width {
			cells = append(cells, "")
		}

		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")

		if i == 0 {
			b.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func tableCell(cell adfNode) string {
	s := strings.ReplaceAll(renderBlocks(cell.Content, " "), "\n", " ")

	return strings.ReplaceAll(s, "|", `\|`)
}

func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}

	return strings.Join(lines, "\n")
}

func panelLabel(panelType string) string {
	if panelType == "" {
		return "Panel"
	}

	return strings.ToUpper(panelType[:1]) + panelType[1:]
}

func mediaPlaceholder(n adfNode) string {
	name := strAttr(n.Attrs, "alt")
	if name == "" {
		name = strAttr(n.Attrs, "url")
	}

	if name == "" {
		name = "unnamed"
	}

	return "[attachment: " + name + "]"
}

func formatDate(ms string) string {
	v, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return unsupported("date")
	}

	return time.UnixMilli(v).UTC().Format(time.DateOnly)
}

// codeFence returns a backtick fence longer than any backtick run in code.
func codeFence(code string) string {
	longest, run := 0, 0

	for _, r := range code {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}

	return strings.Repeat("`", max(3, longest+1))
}

func plainText(nodes []adfNode) string {
	var b strings.Builder

	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(n.Text)
		case "hardBreak":
			b.WriteString("\n")
		default:
			b.WriteString(plainText(n.Content))
		}
	}

	return b.String()
}

func strAttr(attrs map[string]any, key string) string {
	s, _ := attrs[key].(string)

	return s
}

func intAttr(attrs map[string]any, key string, def int) int {
	if f, ok := attrs[key].(float64); ok {
		return int(f)
	}

	return def
}
