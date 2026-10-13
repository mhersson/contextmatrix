package jiraimport

import (
	"encoding/json"
	"fmt"
)

// adfDoc is a one-paragraph Atlassian Document Format body.
func adfDoc(text string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":%q}]}]}`, text))
}
