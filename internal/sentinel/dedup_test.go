package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSimilar(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"reworded same core", "Run the verify script before pushing changes", "Always run the verify script before pushing your changes", true},
		{"unrelated", "Run the verify script before pushing changes", "The database migration needs a rollback plan", false},
		{"case and punctuation", "Use make", "use  MAKE!", true},
		{"short unequal", "Use make", "Use mage", false},
		{"containment", "run the verify script", "Before anything else, run the verify script. Then push the branch and open a pull request for review.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, similar(tt.a, tt.b))
			assert.Equal(t, tt.want, similar(tt.b, tt.a))
		})
	}
}
