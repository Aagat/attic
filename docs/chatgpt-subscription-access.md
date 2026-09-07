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

## Proposed Attic implementation

Add a native Go implementation of the existing `ai.Analyzer` interface in [approval.go](../implementation/attic/internal/ai/approval.go). Keep authorization and refresh in an independent credential component. A first-party Attic login command can run the device flow and persist its own private credential file in the deployment volume.

The adapter needs Responses request encoding, screenshot image input, streamed-response parsing, existing article-result validation, attempt recording, and clear authentication/allowance errors. Validate schema-constrained JSON and screenshot handling against the actual backend before claiming the article-to-PDF deployment works. Configuration changes alone cannot convert the current Chat Completions client into this integration.

Store refresh results atomically, serialize refresh attempts, exclude credentials from logs and Git, and avoid modifying credentials owned by another client. These are proposed implementation requirements rather than claims about backend guarantees.

## Implementation and live validation

Completed on 2026-09-07 after the source investigation above:

- Added native device authorization and serialized token refresh in
  `implementation/attic/internal/subscription`. Credentials belong to Attic and
  are stored privately on its persistent volume.
- Added `AI_PROVIDER=chatgpt`, `CHATGPT_AUTH_FILE`, and `attic login-chatgpt`.
  The existing API-key provider remains available as `AI_PROVIDER=api`.
- Added direct Responses HTTP streaming in `internal/ai/subscription.go`,
  retaining the existing article validation, single repair, and durable attempt
  recording. No external coding-agent process is used.
- Observed that the live backend sends final message content in
  `response.output_item.done` and omits `output` from `response.completed`.
  The adapter assembles those completed items but requires a successful final
  completion event. A regression test reproduces this event shape and rejects
  interrupted streams.
- Native device login and the live image/article compatibility check succeeded
  with `gpt-5.6-luna` using the owner's subscription.
- `https://go.dev/blog/go1.23` completed on its first processing attempt as job
  `7fd96bdf3e7b261d2090b272270911d2`. PostgreSQL records a successful AI attempt
  with model `gpt-5.6-luna` and prompt version `attic-v1`.
- Authenticated download returned a 91,463-byte, four-page A5 PDF containing
  selectable text. SHA-256:
  `165ee07cf9db717845ff700b2dd459eaa04c6bfdf47fab46bbd44aa98db8e6c2`.
  Job state and the identical artifact survived application-container replacement.
- Full Go tests, race checks for AI/authentication/configuration, Go vet, shell
  syntax validation, and container builds passed. Token rotation was tested
  against a fake OAuth server; natural live token expiry has not yet occurred.

The local deployment is healthy at `http://127.0.0.1:18080`. Its setup and
re-login commands are documented in the implementation README. The validation
PDF is saved under ignored `data/validation/go1.23.pdf` for owner inspection.
The first page is readable but repeats the source heading; deduplicating that
content and improving PDF title metadata remain quality-work items.
