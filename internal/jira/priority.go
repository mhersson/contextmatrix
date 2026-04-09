package jira

import "strings"

// priorityMap maps Jira priority names (lowercased) to CM priorities.
var priorityMap = map[string]string{
	"highest":  "critical",
	"critical": "critical",
	"blocker":  "critical",
	"high":     "high",
	"medium":   "medium",
	"normal":   "medium",
	"low":      "low",
	"lowest":   "low",
	"trivial":  "low",
	"minor":    "low",
}

// MapPriority converts a Jira priority name to a CM priority.
// Returns "medium" if the Jira priority is unrecognized or empty.
func MapPriority(jiraPriority string) string {
	if jiraPriority == "" {
		return "medium"
	}
	if p, ok := priorityMap[strings.ToLower(jiraPriority)]; ok {
		return p
	}
	return "medium"
}
