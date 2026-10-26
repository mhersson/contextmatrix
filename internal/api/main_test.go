package api

import (
	"os"
	"testing"
)

// TestMain drops the repository variables git exports to hooks, `rebase
// --exec` and `bisect run`: with GIT_DIR set, the tests' git calls in temp
// dirs would act on the repository running them.
func TestMain(m *testing.M) {
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_PREFIX"} {
		_ = os.Unsetenv(k)
	}

	os.Exit(m.Run())
}
