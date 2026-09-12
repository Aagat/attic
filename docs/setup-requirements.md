# Setup and deployment usability specification

Source: owner-requested Attic task, 2026-09-12; deployment evidence:
`homeserver/docs/attic-deployment-20260912.md`. Work is isolated from production:
no deployment, production credentials, real provider calls or email without opt-in.

## Goal
An owner can deploy supported Compose, connect ChatGPT and configure Kindle in
Settings, and diagnose failures without SSH. Preserve CLI and environment deployments.
The deployed v1.0.0 required CLI OAuth, external SMTP secrets, and Debian Chromium,
Pandoc, XeLaTeX, Poppler, Xvfb and fonts (including lmodern). A scoped AppArmor
profile allowed Chromium namespaces with its sandbox retained. The homepage PDF
failure was an upstream unsupported-article rejection, not a formatter failure.

## Required behavior and acceptance
1. Owner-authenticated ChatGPT Connect, Reconnect, status and Disconnect in Settings.
   Server-side device authorization displays only verification URL/code; bounded
   polling survives page reloads and concurrent requests, with waiting, success,
   cancellation, expiry and retry. Store tokens privately on the persistent volume;
   preserve refresh serialization and old credentials on failed reconnect. Explain
   local removal versus provider revocation. Distinguish absent/revoked credentials,
   unavailable provider, model/permission and quota failures where supported. An
   explicit compatibility check gives meaningful results. Canonical saves and reads
   work without AI. Unauthorized callers cannot inspect or initiate authorization.
2. Owner-configurable SMTP host/port/TLS/username/password/sender/Kindle destination
   when deployment values do not manage them. Validate certificates and inputs;
   never return passwords. Blank password retains it; explicit clearing and rotation.
   Document authoritative environment precedence and label managed fields. Persist
   securely across restarts/upgrades and apply without Compose edits. Separate
   connection/TLS/auth test (no mail) from explicit email test with visible recipient.
   Explain Amazon approved sender and app passwords. SMTP acceptance is not device
   receipt. Configuration never automatically requests delivery of saved items/jobs.
3. Durable, safe, stage-specific item/job errors for capture, AI approval/connection,
   PDF format/validation, search and SMTP, including reason, next action and job ID.
   Unsupported content remains rejected with an accurate article eligibility message.
   Preserve saved content and last good artifacts. Truthful queued/running/ready/failed
   states, useful reload behavior and appropriate retries without recapture/duplicate
   email. Deterministic missing-auth, unsupported-content, formatter and SMTP fixtures.
4. Versioned OCI runtime and supported Compose with full tools/fonts/UI/migrations.
   Document resources, writable volumes/UIDs, secrets, proxy/ports, protected owner
   bootstrap, upgrades and migration-aware recovery. Existing dedicated PostgreSQL
   database/role and shared Meilisearch with index-scoped key must work. Optional
   integrations must not prevent canonical saves/reads. Sandboxed Chromium/AppArmor
   diagnostics; no privileged/no-sandbox/host-wide weakening default. Clean runtime
   validation must exercise real capture and Attic's formatter/template, persistence
   and upgrade, not merely binary start or generic Pandoc. Ask before provisioning
   services beyond the observed runtime.
5. Settings overview of AI, PDF/browser, search and mail with actionable next steps.
   Liveness/readiness must not imply optional integration readiness. PostgreSQL plus
   application files/configuration are canonical; Meilisearch indexes disposable.
   Owner reindex action and status verify applied work, not just queue acceptance;
   recovery never removes another application's indexes.

Deliver frontend/backend/configuration/persistence/docs and meaningful configuration,
authorization/workflow tests plus image integration checks. Report validation evidence,
remaining limitations and operator upgrade/homeserver handoff steps honestly.
