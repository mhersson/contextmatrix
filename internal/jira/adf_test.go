package jira

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func q(s string) string {
	b, _ := json.Marshal(s)

	return string(b)
}

func doc(blocks ...string) json.RawMessage {
	return json.RawMessage(`{"type":"doc","version":1,"content":[` + strings.Join(blocks, ",") + `]}`)
}

func node(typ, attrs string, content ...string) string {
	s := `{"type":` + q(typ)
	if attrs != "" {
		s += `,"attrs":` + attrs
	}

	return s + `,"content":[` + strings.Join(content, ",") + `]}`
}

func para(inline ...string) string { return node("paragraph", "", inline...) }

func text(s string, marks ...string) string {
	return `{"type":"text","text":` + q(s) + `,"marks":[` + strings.Join(marks, ",") + `]}`
}

func mark(typ, attrs string) string {
	if attrs == "" {
		return `{"type":` + q(typ) + `}`
	}

	return `{"type":` + q(typ) + `,"attrs":` + attrs + `}`
}

func item(blocks ...string) string { return node("listItem", "", blocks...) }

func TestADFToMarkdown(t *testing.T) {
	tests := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{name: "nil", in: nil, want: ""},
		{name: "json null", in: json.RawMessage(`null`), want: ""},
		{name: "empty doc", in: doc(), want: ""},
		{name: "invalid json", in: json.RawMessage(`{`), want: "[unsupported Jira content: invalid document]"},
		{name: "paragraph", in: doc(para(text("Hello world"))), want: "Hello world"},
		{name: "blocks separated by a blank line", in: doc(node("heading", `{"level":1}`, text("T")), para(text("p"))), want: "# T\n\np"},
		{name: "empty paragraph skipped", in: doc(para(text("a")), para(), para(text("b"))), want: "a\n\nb"},
		{name: "heading level", in: doc(node("heading", `{"level":2}`, text("Title"))), want: "## Title"},
		{
			name: "marks",
			in: doc(para(
				text("b", mark("strong", "")), text(" "), text("i", mark("em", "")), text(" "),
				text("c", mark("code", "")), text(" "), text("s", mark("strike", "")), text(" "),
				text("site", mark("link", `{"href":"https://e.com"}`)), text(" "),
				text("both", mark("strong", ""), mark("link", `{"href":"https://e.com"}`)),
			)),
			want: "**b** _i_ `c` ~~s~~ [site](https://e.com) [**both**](https://e.com)",
		},
		{name: "mark whitespace stays outside", in: doc(para(text("bold ", mark("strong", "")), text("after"))), want: "**bold** after"},
		{name: "code mark with backtick", in: doc(para(text("a`b", mark("code", "")))), want: "`` a`b ``"},
		{name: "unknown mark keeps text", in: doc(para(text("u", mark("underline", "")))), want: "u"},
		{name: "bullet list", in: doc(node("bulletList", "", item(para(text("a"))), item(para(text("b"))))), want: "- a\n- b"},
		{
			name: "nested bullet list",
			in: doc(node("bulletList", "",
				item(para(text("a")), node("bulletList", "", item(para(text("a1"))))),
				item(para(text("b"))),
			)),
			want: "- a\n  - a1\n- b",
		},
		{name: "ordered list start", in: doc(node("orderedList", `{"order":3}`, item(para(text("x"))), item(para(text("y"))))), want: "3. x\n4. y"},
		{
			name: "list item with code block",
			in:   doc(node("bulletList", "", item(para(text("run")), node("codeBlock", "", text("make test"))))),
			want: "- run\n  ```\n  make test\n  ```",
		},
		{name: "code block language", in: doc(node("codeBlock", `{"language":"go"}`, text("fmt.Println(1)"))), want: "```go\nfmt.Println(1)\n```"},
		{name: "code block containing a fence", in: doc(node("codeBlock", "", text("a ``` b"))), want: "````\na ``` b\n````"},
		{name: "blockquote", in: doc(node("blockquote", "", para(text("q1")), para(text("q2")))), want: "> q1\n>\n> q2"},
		{name: "rule", in: doc(`{"type":"rule"}`), want: "---"},
		{
			name: "table",
			in: doc(node("table", "",
				node("tableRow", "", node("tableHeader", "", para(text("H1"))), node("tableHeader", "", para(text("H2")))),
				node("tableRow", "", node("tableCell", "", para(text("a|b"))), node("tableCell", "", para(text("c")))),
			)),
			want: "| H1 | H2 |\n| --- | --- |\n| a\\|b | c |",
		},
		{name: "panel", in: doc(node("panel", `{"panelType":"warning"}`, para(text("careful")))), want: "> **Warning**\n>\n> careful"},
		{name: "hard break", in: doc(para(text("a"), `{"type":"hardBreak"}`, text("b"))), want: "a  \nb"},
		{name: "mention", in: doc(para(`{"type":"mention","attrs":{"id":"1","text":"@Ada Lovelace"}}`)), want: "@Ada Lovelace"},
		{name: "mention without at", in: doc(para(`{"type":"mention","attrs":{"text":"Ada"}}`)), want: "@Ada"},
		{name: "emoji text", in: doc(para(`{"type":"emoji","attrs":{"shortName":":grin:","text":"😁"}}`)), want: "😁"},
		{name: "emoji short name", in: doc(para(`{"type":"emoji","attrs":{"shortName":":smile:"}}`)), want: ":smile:"},
		{name: "inline card", in: doc(para(`{"type":"inlineCard","attrs":{"url":"https://e.com/x"}}`)), want: "<https://e.com/x>"},
		{name: "status", in: doc(para(`{"type":"status","attrs":{"text":"IN REVIEW","color":"blue"}}`)), want: "[IN REVIEW]"},
		{name: "date", in: doc(para(`{"type":"date","attrs":{"timestamp":"1760227200000"}}`)), want: "2025-10-12"},
		{
			name: "media single with name",
			in:   doc(node("mediaSingle", "", `{"type":"media","attrs":{"id":"abc","type":"file","alt":"screenshot.png"}}`)),
			want: "[attachment: screenshot.png]",
		},
		{
			name: "media group",
			in:   doc(node("mediaGroup", "", `{"type":"media","attrs":{"alt":"a.txt"}}`, `{"type":"media","attrs":{}}`)),
			want: "[attachment: a.txt]\n[attachment: unnamed]",
		},
		{
			name: "unknown block keeps children",
			in:   doc(node("expand", `{"title":"More"}`, para(text("hidden")))),
			want: "[unsupported Jira content: expand]\n\nhidden",
		},
		{
			name: "nested unknown containers keep children",
			in: doc(node("layoutSection", "",
				node("layoutColumn", "", para(text("left"))),
				node("expand", "", para(text("right"))))),
			want: "[unsupported Jira content: layoutSection]\n\n[unsupported Jira content: layoutColumn]\n\nleft\n\n[unsupported Jira content: expand]\n\nright",
		},
		{name: "unknown without content", in: doc(node("extension", `{}`)), want: "[unsupported Jira content: extension]"},
		{
			name: "unknown inline keeps children",
			in:   doc(para(text("see "), node("inlineExtension", "", text("inner")))),
			want: "see [unsupported Jira content: inlineExtension]inner",
		},
		{name: "unknown inline", in: doc(para(text("see "), `{"type":"inlineExtension","attrs":{}}`)), want: "see [unsupported Jira content: inlineExtension]"},
		{name: "empty raw message", in: json.RawMessage(``), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ADFToMarkdown(tt.in))
		})
	}
}
