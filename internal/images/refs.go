package images

import "regexp"

// mdImage matches a markdown image reference, `![alt](url)`, capturing the
// URL. Square brackets in alt text are allowed except the `]` that closes it.
var mdImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

// cmImageURL matches a relative (`/api/images/<id>`) or absolute
// (`https://host/api/images/<id>`) URL served by this server, capturing the
// id and allowing an ignored query string.
var cmImageURL = regexp.MustCompile(`^(?:https?://[^/]+)?/api/images/(` + IDPatternFragment + `)(\?[^)]*)?$`)

// ReferencedIDs returns every unique cm-server image id referenced by a
// markdown image in body, in order of first appearance. Nil when none.
func ReferencedIDs(body string) []string {
	matches := mdImage.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	ids := make([]string, 0, len(matches))

	for _, m := range matches {
		sub := cmImageURL.FindStringSubmatch(m[1])
		if len(sub) < 2 {
			continue
		}

		if _, ok := seen[sub[1]]; ok {
			continue
		}

		seen[sub[1]] = struct{}{}
		ids = append(ids, sub[1])
	}

	if len(ids) == 0 {
		return nil
	}

	return ids
}
