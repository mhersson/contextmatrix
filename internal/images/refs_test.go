package images

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReferencedIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{"relative URL", "see ![shot](/api/images/aabbccddeeff0011)", []string{"aabbccddeeff0011"}},
		{"absolute URL with host", "![](http://localhost:8080/api/images/0123456789abcdef)", []string{"0123456789abcdef"}},
		{"https URL", "![alt](https://cm.example/api/images/0123456789abcdef)", []string{"0123456789abcdef"}},
		{"query string ignored", "![](/api/images/aabbccddeeff0011?w=1)", []string{"aabbccddeeff0011"}},
		{"external image skipped", "![ours](/api/images/aaaaaaaaaaaaaaaa) ![theirs](https://imgur.com/foo.png)", []string{"aaaaaaaaaaaaaaaa"}},
		{"dedup keeps first order", "![](/api/images/bbbbbbbbbbbbbbbb) ![](/api/images/aaaaaaaaaaaaaaaa) ![](/api/images/bbbbbbbbbbbbbbbb)", []string{"bbbbbbbbbbbbbbbb", "aaaaaaaaaaaaaaaa"}},
		{"wrong id length", "![](/api/images/abc)", nil},
		{"uppercase hex rejected", "![](/api/images/AABBCCDDEEFF0011)", nil},
		{"plain link is not an image", "[text](/api/images/aabbccddeeff0011)", nil},
		{"empty", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ReferencedIDs(tt.body))
		})
	}
}

func TestReferencedIDs_IsNotCapped(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for i := range 25 {
		fmt.Fprintf(&b, "![](/api/images/%016x)\n", i)
	}

	assert.Len(t, ReferencedIDs(b.String()), 25)
}
