package board

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func snapshotBase() *Card {
	return &Card{
		Title:  "t",
		Verify: &VerifyConfig{Command: "make test", TimeoutSeconds: 600},
		Body:   "intro\n## Notes\nn\n",
	}
}

func TestTakeEditSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Card)
		want   []string
	}{
		{"title", func(c *Card) { c.Title = "other" }, []string{"title"}},
		{"verify timeout", func(c *Card) { c.Verify.TimeoutSeconds = 900 }, []string{"verify"}},
		{"autonomous", func(c *Card) { c.Autonomous = true }, []string{"autonomous"}},
		{"intro", func(c *Card) { c.Body = "changed\n## Notes\nn\n" }, []string{"intro"}},
		{"section edited", func(c *Card) { c.Body = "intro\n## Notes\nedited\n" }, []string{"## Notes"}},
		{"section added", func(c *Card) { c.Body += "## Extra\ne\n" }, []string{"## Extra"}},
		{"not editable", func(c *Card) {
			c.Phase = "p"
			c.WorkerStatus = "running"
			c.ActivityLog = append(c.ActivityLog, ActivityEntry{Action: "x"})
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := TakeEditSnapshot(snapshotBase())
			c := snapshotBase()
			tt.mutate(c)
			after := TakeEditSnapshot(c)

			var differ []string

			for k, v := range after {
				if base[k] != v {
					differ = append(differ, k)
				}
			}

			for k := range base {
				if _, ok := after[k]; !ok {
					differ = append(differ, k)
				}
			}

			assert.ElementsMatch(t, tt.want, differ)
		})
	}
}

func TestTakeEditSnapshot_NilVerifyEqualsZero(t *testing.T) {
	a := &Card{Verify: nil}
	b := &Card{Verify: &VerifyConfig{}}
	assert.Equal(t, TakeEditSnapshot(a), TakeEditSnapshot(b))
}

func TestTakeEditSnapshot_RepeatedHeading(t *testing.T) {
	snap := TakeEditSnapshot(&Card{Body: "## Notes\na\n## Notes\nb\n"})
	assert.Contains(t, snap, "## Notes")
	assert.Contains(t, snap, "## Notes #2")
	assert.NotEqual(t, snap["## Notes"], snap["## Notes #2"])
}
