package config

// Defaults returns the complete default configuration: every key the loader
// accepts, with the value Load would apply when the key is absent. It is the
// schema source for contextmatrix-setup, so three things differ from Load:
//
//   - backends.agent and backends.chat are present and disabled so their
//     key sets are visible;
//   - path-valued keys whose default depends on the host (auth.db_path,
//     auth.master_key_file, images.db_path, op_store.db_path) stay empty;
//   - nothing is read from disk or the environment.
func Defaults() *Config {
	cfg := defaults()

	applyChatDefaults(cfg)
	applyBestOfNDefaults(cfg)
	applyMobDefaults(cfg)

	disabled := false
	cfg.Backends.Agent = &AgentBackendConfig{
		Enabled:           &disabled,
		ReconcileInterval: "60s",
	}
	cfg.Backends.Chat = &ChatBackendConfig{Enabled: &disabled}

	// Empty maps print as {} rather than null; the installer treats {} as
	// an opaque node whose user content is kept verbatim.
	cfg.TokenCosts = map[string]ModelRate{}

	// nil means enabled; print that default explicitly so the key shows
	// "true" instead of "null".
	enabled := true
	cfg.Mob.ExecuteCheckpointsEnabled = &enabled

	return cfg
}
