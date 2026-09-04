package config

// Defaults returns the complete default configuration: every key the loader
// accepts, with the value Load would apply when the key is absent. It runs
// the same apply*Defaults chain Load runs, in Load's order, rather than
// re-deriving the values, so a default added upstream to any of those
// helpers reaches the schema without a second edit here.
//
// Both backend entries are declared before the chain runs and left enabled
// while it runs, because applyBackendDefaults only fills an entry that is
// enabled; they are switched off afterwards so their key sets are visible
// without turning either backend on. The four path-valued keys whose default
// depends on the host (auth.db_path, auth.master_key_file, images.db_path,
// op_store.db_path) are blanked after the chain, so the schema carries no
// machine-specific path. Nothing is read from disk or the environment.
func Defaults() *Config {
	cfg := defaults()

	// A nil Enabled means enabled, so these entries are live for the chain.
	cfg.Backends.Agent = &AgentBackendConfig{}
	cfg.Backends.Chat = &ChatBackendConfig{}

	applyChatDefaults(cfg)
	applyImagesDefaults(cfg)
	applyOpStoreDefaults(cfg)
	applyBestOfNDefaults(cfg)
	applyMobDefaults(cfg)
	applyAuthDefaults(cfg)
	applyBackendDefaults(cfg)

	disabled := false
	cfg.Backends.Agent.Enabled = &disabled
	cfg.Backends.Chat.Enabled = &disabled

	cfg.Auth.DBPath, cfg.Auth.MasterKeyFile = "", ""
	cfg.Images.DBPath, cfg.OpStore.DBPath = "", ""

	// Empty maps print as {} rather than null; the installer treats {} as
	// an opaque node whose user content is kept verbatim.
	cfg.TokenCosts = map[string]ModelRate{}

	// nil means enabled; print that default explicitly so the key shows
	// "true" instead of "null".
	enabled := true
	cfg.Mob.ExecuteCheckpointsEnabled = &enabled

	return cfg
}
