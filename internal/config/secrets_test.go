package config

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isSecretTag(tag string) bool {
	name, _, _ := strings.Cut(tag, ",")

	return name == "api_key" || name == "token" || name == "oauth_token" || strings.HasSuffix(name, "_api_key")
}

// fillSecrets sets every secret-tagged string reachable from v to a distinct
// value, allocating pointers, slice elements and map entries on the way.
func fillSecrets(v reflect.Value, n *int, want *[]string) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}

		fillSecrets(v.Elem(), n, want)
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}

			fv := v.Field(i)
			if fv.Kind() == reflect.String && isSecretTag(f.Tag.Get("yaml")) {
				*n++
				s := "secret-value-" + strconv.Itoa(*n)
				fv.SetString(s)
				*want = append(*want, s)

				continue
			}

			fillSecrets(fv, n, want)
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String {
			return
		}

		v.Set(reflect.MakeSlice(v.Type(), 2, 2))

		for i := range v.Len() {
			fillSecrets(v.Index(i), n, want)
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}

		v.Set(reflect.MakeMap(v.Type()))

		e := reflect.New(v.Type().Elem()).Elem()
		fillSecrets(e, n, want)
		v.SetMapIndex(reflect.ValueOf("k").Convert(v.Type().Key()), e)
	}
}

func TestSecretValuesCoversEverySecretField(t *testing.T) {
	var (
		cfg  Config
		n    int
		want []string
	)

	fillSecrets(reflect.ValueOf(&cfg).Elem(), &n, &want)
	require.NotEmpty(t, want)

	assert.ElementsMatch(t, want, cfg.SecretValues())
}

func TestSecretValuesSkipsEmpty(t *testing.T) {
	assert.Empty(t, (&Config{}).SecretValues())
}
