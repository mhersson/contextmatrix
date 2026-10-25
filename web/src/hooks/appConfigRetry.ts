// Bounded retry around the initial app-config fetch. This call decides the
// auth mode (multi vs none), so a single transient blip against a multi-mode
// server must not mis-detect it as none-mode and drop the user onto the
// no-auth path. 3 attempts total, short backoff between them (adds at most
// ~400ms in the worst case before falling back).
export const APP_CONFIG_RETRY_BACKOFFS_MS = [100, 300];
