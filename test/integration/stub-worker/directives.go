package main

import (
	"bufio"
	"strings"
)

// directives carries the parsed STUB-DIRECTIVE markers from the card body.
type directives struct {
	skipHeartbeat    bool
	hangAfterClaim   bool
	promoteBehaviour string // "respect" (default) or "ignore"
}

// parseDirectives scans the markdown body for lines of the form
//
//	STUB-DIRECTIVE: name=value
//
// and returns the accumulated config. Unknown names are ignored; the
// stub falls back to defaults. Lines without `=` set a boolean flag.
func parseDirectives(body string) directives {
	d := directives{promoteBehaviour: "respect"}
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "STUB-DIRECTIVE:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "STUB-DIRECTIVE:"))
		name, value, hasEq := strings.Cut(rest, "=")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		switch name {
		case "skip-heartbeat":
			d.skipHeartbeat = !hasEq || isTrue(value)
		case "hang-after-claim":
			d.hangAfterClaim = !hasEq || isTrue(value)
		case "promote-behaviour":
			if value == "respect" || value == "ignore" {
				d.promoteBehaviour = value
			}
		}
	}
	return d
}

func isTrue(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
