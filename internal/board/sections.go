package board

import "strings"

// UpsertSection replaces the "## <heading>" block in body (up to the next H2
// outside code fences) or appends it when absent. heading is the H2 title
// without the "## " prefix. Idempotent: applying the same heading and
// content twice produces the same output, so retries and resume paths never
// duplicate sections.
func UpsertSection(body, heading, content string) string {
	marker := "## " + heading
	section := marker + "\n\n" + strings.TrimRight(content, "\n") + "\n"

	lines := bodyLines(body)

	start, end := -1, len(lines)
	for i, line := range lines {
		if !line.heading {
			continue
		}

		if start != -1 {
			end = i

			break
		}

		if line.trimmed == marker {
			start = i
		}
	}

	if start == -1 {
		if strings.TrimSpace(body) == "" {
			return section
		}

		return strings.TrimRight(body, "\n") + "\n\n" + section
	}

	prefix := joinLines(lines[:start])

	suffix := joinLines(lines[end:])
	if suffix != "" {
		return prefix + section + "\n" + suffix
	}

	return prefix + section
}

// Section is one H2 block of a card body; Heading omits the "## " prefix and
// Content runs to the next H2, byte for byte.
type Section struct{ Heading, Content string }

// BodySections splits body into the text before the first H2 and its H2
// sections, by the same line rules as UpsertSection.
func BodySections(body string) (intro string, sections []Section) {
	var introB strings.Builder

	for _, line := range bodyLines(body) {
		switch {
		case line.heading:
			sections = append(sections, Section{Heading: strings.TrimSpace(line.trimmed[3:])})
		case len(sections) == 0:
			introB.WriteString(line.text)
		default:
			sections[len(sections)-1].Content += line.text
		}
	}

	return introB.String(), sections
}

// bodyLine is one line of a card body as the section rules see it.
type bodyLine struct {
	text    string // the line's bytes, its newline included
	trimmed string // without the line ending, so CRLF lines match LF markers
	heading bool   // an H2 heading: "## " at the line start, outside code fences
}

// bodyLines splits body after each newline and marks its H2 headings. Fence
// delimiters (``` or ~~~) are matched left-trimmed, so an indented fence still
// hides its contents, but a heading must start the line: an indented "## X"
// is never one, matching internal/mcp/bodysections.go's convention.
func bodyLines(body string) []bodyLine {
	parts := strings.SplitAfter(body, "\n")
	lines := make([]bodyLine, len(parts))
	inFence := false

	for i, text := range parts {
		trimmed := strings.TrimRight(text, "\r\n")
		fence := strings.TrimLeft(trimmed, " \t")

		switch {
		case strings.HasPrefix(fence, "```") || strings.HasPrefix(fence, "~~~"):
			inFence = !inFence
		case !inFence && strings.HasPrefix(trimmed, "## "):
			lines[i].heading = true
		}

		lines[i].text, lines[i].trimmed = text, trimmed
	}

	return lines
}

func joinLines(lines []bodyLine) string {
	var b strings.Builder

	for _, l := range lines {
		b.WriteString(l.text)
	}

	return b.String()
}
