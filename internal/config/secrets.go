package config

// SecretValues returns every non-empty secret the config holds, so text bound
// for publication can be checked against them. A new secret field needs a line
// here; TestSecretValuesCoversEverySecretField fails until it has one.
func (c *Config) SecretValues() []string {
	candidates := []string{
		c.MCPAPIKey,
		c.ClaudeCode.OAuthToken,
		c.LLMEndpoint.APIKey,
		c.GitHub.PAT.Token,
		c.Jira.Token,
	}

	if a := c.Backends.Agent; a != nil {
		candidates = append(candidates, a.APIKey, a.AAAPIKey)
	}

	if ch := c.Backends.Chat; ch != nil {
		candidates = append(candidates, ch.APIKey)
	}

	if ar := c.Backends.Architect; ar != nil {
		candidates = append(candidates, ar.APIKey, ar.ResearchAPIKey)
	}

	for _, g := range c.Mob.Guests {
		candidates = append(candidates, g.Token)
	}

	for _, p := range c.Knowledge.Providers {
		candidates = append(candidates, p.Token)
	}

	var out []string

	for _, s := range candidates {
		if s != "" {
			out = append(out, s)
		}
	}

	return out
}
