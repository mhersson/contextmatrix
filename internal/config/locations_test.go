package config

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isLocationTag(tag string) bool {
	name, _, _ := strings.Cut(tag, ",")

	return name == "url" || name == "host" || strings.HasSuffix(name, "_url")
}

// fillLocations sets every location-tagged string reachable from v to a
// distinct value, allocating pointers, slice elements and map entries on the way.
func fillLocations(v reflect.Value, n *int, want *[]string) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}

		fillLocations(v.Elem(), n, want)
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}

			fv := v.Field(i)
			if fv.Kind() == reflect.String && isLocationTag(f.Tag.Get("yaml")) {
				*n++
				s := "https://location-" + strconv.Itoa(*n) + ".example"
				fv.SetString(s)
				*want = append(*want, s)

				continue
			}

			fillLocations(fv, n, want)
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String {
			return
		}

		v.Set(reflect.MakeSlice(v.Type(), 2, 2))

		for i := range v.Len() {
			fillLocations(v.Index(i), n, want)
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}

		v.Set(reflect.MakeMap(v.Type()))

		e := reflect.New(v.Type().Elem()).Elem()
		fillLocations(e, n, want)
		v.SetMapIndex(reflect.ValueOf("k").Convert(v.Type().Key()), e)
	}
}

func TestLocationValuesCoversEveryLocationField(t *testing.T) {
	var (
		cfg  Config
		n    int
		want []string
	)

	fillLocations(reflect.ValueOf(&cfg).Elem(), &n, &want)
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
