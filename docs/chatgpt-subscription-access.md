# Direct ChatGPT subscription access

Research date: 2026-09-07. Source inspection only; no credentials were read and no authenticated requests were made.

OpenCode documents browser login with ChatGPT Plus/Pro. Its implementation calls the Codex backend directly, so a native Attic adapter would not need `codex exec` or another agent process. [OpenCode provider documentation](https://opencode.ai/docs/providers/#openai)

## Observed OpenCode protocol

- Public OAuth client ID: `app_EMoamEEZ73f0CkXaXp7hrann`; issuer: `https://auth.openai.com`.
- Browser flow: authorization code with S256 PKCE, random state, `/oauth/authorize`, scopes `openid profile email offline_access`, callback `http://localhost:1455/auth/callback`; exchange at `/oauth/token`.
- Headless flow: POST `/api/accounts/deviceauth/usercode`; user signs in at `/codex/device`; poll `/api/accounts/deviceauth/token`; exchange the resulting authorization code and verifier at `/oauth/token` using redirect URI `/deviceauth/callback` on the issuer.
- Store access token, refresh token, expiry, and account ID. Refresh uses `/oauth/token` with `grant_type=refresh_token`; concurrent refreshes are deduplicated.
- Requests go to `https://chatgpt.com/backend-api/codex/responses` with bearer access token and `ChatGPT-Account-Id`. HTTP is supported; WebSocket transport is optional. This is Responses transport, not ordinary API-key Chat Completions.
- Model selection is filtered for OAuth, and maximum output tokens are removed by the plugin.

These are observed implementation details, not a stable public API contract. [OpenCode Codex plugin](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/plugin/openai/codex.ts)

Kilo also implements the same issuer, client ID, backend endpoint, browser PKCE flow, and direct headless device flow. [Kilo Codex plugin](https://github.com/Kilo-Org/kilocode/blob/main/packages/opencode/src/plugin/openai/codex.ts)

## Subscription and support boundary

Official OpenAI documentation distinguishes ChatGPT subscription access from separately billed API-key access. It documents automatic token refresh and device login, which may need enabling in account security settings or workspace permissions. It recommends API keys for programmatic CLI workflows. The inspected documentation does not establish a supported general-purpose subscription API for arbitrary applications or guarantee that Attic's article workload is entitled to use this backend. Account eligibility and remaining allowance must be verified by an authenticated test. [Official OpenAI authentication documentation](https://learn.chatgpt.com/docs/auth)

## Attic implementation

[`internal/subscription`](../internal/subscription) owns device authorization,
private credential storage and serialized, atomic token refresh.
[`internal/ai/subscription.go`](../internal/ai/subscription.go) implements direct
Responses HTTP streaming, screenshot input, article validation and durable
attempt recording. Set `AI_PROVIDER=chatgpt` and use `attic login-chatgpt`;
see the [setup instructions](../README.md#chatgpt-subscription-login).

The backend can send final message content in `response.output_item.done` while
omitting `output` from `response.completed`. The adapter assembles completed
items and requires a successful final completion event. Regression tests cover
that event shape, interrupted streams and credential refresh.
