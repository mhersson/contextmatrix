# Jira import

ContextMatrix connects to one Jira Cloud site per instance through the Jira REST
API v3. This page covers configuring that site and its credentials.
`GET /api/jira/status` reports whether the connection works.

## Configuration

| Key                 | Env                               | Default            | Rule                                                                                                                                |
| ------------------- | --------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| `jira.site_url`     | `CONTEXTMATRIX_JIRA_SITE_URL`     | empty              | `https` URL of the Jira Cloud site, e.g. `https://acme.atlassian.net`. Empty disables Jira and every other `jira.*` key is ignored. |
| `jira.api_base_url` | `CONTEXTMATRIX_JIRA_API_BASE_URL` | empty (`site_url`) | `https`. Set to `https://api.atlassian.com/ex/jira/<cloudId>` for service-account scoped tokens.                                    |
| `jira.auth_mode`    | `CONTEXTMATRIX_JIRA_AUTH_MODE`    | `basic`            | `basic` or `bearer`.                                                                                                                |
| `jira.email`        | `CONTEXTMATRIX_JIRA_EMAIL`        | empty              | Required for `basic`.                                                                                                               |
| `jira.token`        | `CONTEXTMATRIX_JIRA_TOKEN`        | empty              | Required. Supply it through the env var.                                                                                            |

When `site_url` is set, startup fails if a URL is not `https`, if the fields
`auth_mode` needs are missing, or if a map key is not a Jira project key.

The token is read once at startup and lives only inside the Jira client. It
never appears in API responses, logs or errors. Changing it means changing the
config or env var and restarting.

## Authentication

| Mode                | Credential                                                                   | Base URL                                              |
| ------------------- | ---------------------------------------------------------------------------- | ----------------------------------------------------- |
| `basic`             | Atlassian account email + API token: a personal token, or a bot user's token | `site_url`                                            |
| `basic` or `bearer` | Atlassian service account API token                                          | `api_base_url` set to the `api.atlassian.com` gateway |

- **Personal token for testing.** Create one at
  <https://id.atlassian.com/manage-profile/security/api-tokens> with a short
  expiry. Set `email` to your own address. Jira records every action CM takes as
  yours.
- **Service account.** An org admin creates it in Atlassian Administration and
  issues it an API token, which must go through the gateway URL. Look up the
  site's `cloudId` at `https://<site>/_edge/tenant_info`.
- **Login CAPTCHA.** After repeated failed logins Jira blocks basic auth for the
  account until someone signs in through a browser. The status endpoint then
  reports `auth_error: captcha_required`.

Check the connection:

```bash
curl -s -H 'X-Requested-With: contextmatrix' localhost:8080/api/jira/status
```
