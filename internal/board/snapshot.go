package board

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// EditSnapshot maps each human-editable part of a card to a sha256 hex digest.
// Keys: "title", "verify", "autonomous", "intro", and "## <Heading>" per body
// section (a repeated heading gets " #2", " #3").
type EditSnapshot map[string]string

// TakeEditSnapshot hashes the parts of c a human edits. Verify is the card's
// own, not the resolved one; nil and zero hash the same.
func TakeEditSnapshot(c *Card) EditSnapshot {
	var verify VerifyConfig
	if c.Verify != nil {
		verify = *c.Verify
	}

	// A struct of strings, ints and string slices always marshals.
	verifyJSON, _ := json.Marshal(verify)

	intro, sections := BodySections(c.Body)

	snap := EditSnapshot{
		"title":      hashText(c.Title),
		"verify":     hashText(string(verifyJSON)),
		"autonomous": hashText(fmt.Sprint(c.Autonomous)),
		"intro":      hashText(intro),
	}

	seen := map[string]int{}

	for _, s := range sections {
		key := "## " + s.Heading

		seen[key]++
		if n := seen[key]; n > 1 {
			key = fmt.Sprintf("%s #%d", key, n)
		}

		snap[key] = hashText(s.Content)
	}

	return snap
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}
