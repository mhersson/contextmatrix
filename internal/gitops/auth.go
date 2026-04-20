package gitops

import "fmt"

// GitAuthEnv returns the environment variables to inject before every shell git
// invocation. For ssh mode (or any unrecognised mode) it returns nil so that
// the process inherits the caller's environment unchanged. For pat mode it
// injects GIT_CONFIG_* entries that set http.extraheader to an Authorization
// Bearer header, and GIT_TERMINAL_PROMPT=0 so a missing/expired PAT fails
// fast instead of blocking on an interactive prompt.
func GitAuthEnv(mode, token string) []string {
	if mode != "pat" {
		return nil
	}

	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraheader",
		fmt.Sprintf("GIT_CONFIG_VALUE_0=Authorization: Bearer %s", token),
		"GIT_TERMINAL_PROMPT=0",
	}
}
