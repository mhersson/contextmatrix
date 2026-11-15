package config

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isSecretTag(name string) bool {
	return name == "api_key" || name == "token" || name == "oauth_token" || strings.HasSuffix(name, "_api_key")
}

func TestSecretValuesCoversEverySecretField(t *testing.T) {
	var cfg Config

	want := fillTagged(reflect.ValueOf(&cfg).Elem(), isSecretTag, func(n int) string { return "secret-value-" + strconv.Itoa(n) })
	require.NotEmpty(t, want)

	assert.ElementsMatch(t, want, cfg.SecretValues())
}

func TestSecretValuesSkipsEmpty(t *testing.T) {
	assert.Empty(t, (&Config{}).SecretValues())
}
