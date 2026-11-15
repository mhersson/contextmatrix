package config

// LocationValues returns every URL and host the config holds, so text bound
// for publication can be checked for them. A new URL or host field needs a
// line here; TestLocationValuesCoversEveryLocationField fails until it has one.
func (c *Config) LocationValues() []string {
	candidates := []string{
		c.LLMEndpoint.BaseURL,
		c.GitHub.Host,
		c.GitHub.APIBaseURL,
		c.Jira.SiteURL,
		c.Jira.APIBaseURL,
		c.TaskSkills.GitRemoteURL,
	}

	for _, b := range c.Boards {
		candidates = append(candidates, b.GitRemoteURL)
	}

	if a := c.Backends.Agent; a != nil {
		candidates = append(candidates, a.URL)
	}

	if ch := c.Backends.Chat; ch != nil {
		candidates = append(candidates, ch.URL)
	}

	if ar := c.Backends.Architect; ar != nil {
		candidates = append(candidates, ar.URL)
	}

	for _, g := range c.Mob.Guests {
		candidates = append(candidates, g.URL)
	}

	for _, p := range c.Knowledge.Providers {
		candidates = append(candidates, p.URL)
	}

	return nonEmpty(candidates)
}

// RepoURLs returns the config's own git remotes: every boards repository's
// and the task-skills repository's.
func (c *Config) RepoURLs() []string {
	candidates := []string{c.TaskSkills.GitRemoteURL}

	for _, b := range c.Boards {
		candidates = append(candidates, b.GitRemoteURL)
	}

	return nonEmpty(candidates)
}

func nonEmpty(values []string) []string {
	var out []string

	for _, s := range values {
		if s != "" {
			out = append(out, s)
		}
	}

	return out
}
