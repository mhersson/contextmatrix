package config

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isLocationTag(name string) bool {
	return name == "url" || name == "host" || strings.HasSuffix(name, "_url")
}

func TestLocationValuesCoversEveryLocationField(t *testing.T) {
	var cfg Config

	want := fillTagged(reflect.ValueOf(&cfg).Elem(), isLocationTag, func(n int) string {
		return "https://location-" + strconv.Itoa(n) + ".example"
	})
	require.NotEmpty(t, want)

	assert.ElementsMatch(t, want, cfg.LocationValues())
}

func TestLocationValuesSkipsEmpty(t *testing.T) {
	assert.Empty(t, (&Config{}).LocationValues())
}

func TestRepoURLs(t *testing.T) {
	cfg := Config{
		Boards: Boards{
			{Name: "a", GitRemoteURL: "https://github.com/acme/boards-a.git"},
			{Name: "b"},
			{Name: "c", GitRemoteURL: "https://github.com/acme/boards-c.git"},
		},
		TaskSkills: TaskSkillsConfig{GitRemoteURL: "https://github.com/acme/skills.git"},
	}

	assert.ElementsMatch(t, []string{
		"https://github.com/acme/boards-a.git",
		"https://github.com/acme/boards-c.git",
		"https://github.com/acme/skills.git",
	}, cfg.RepoURLs())
	assert.Empty(t, (&Config{}).RepoURLs())
}
