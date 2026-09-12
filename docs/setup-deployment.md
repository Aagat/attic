# Owner setup and deployment

This change adds Settings setup to the post-v1.0.0 application. The full requested
scope is recorded in [setup-requirements.md](setup-requirements.md). Do not treat
an unreleased image tag or an unexecuted integration check as validated deployment.

## Quickstart with existing services

Use a Linux x86-64 Docker Engine host with Compose v2, at least two CPU cores,
2 GiB available application memory plus capacity for the existing PostgreSQL and
search services, and sufficient persistent storage for captures/PDFs. The base
Compose limits Attic to 2 GiB and two CPUs. Chromium/PDF concurrency can require
more memory for large articles. The supported source image includes Chromium,
Pandoc, XeLaTeX, Poppler, Xvfb, fontconfig and Latin Modern/Noto fonts, embedded UI
and immutable migration SQL. The Alpine runtime's Latin Modern source is the
TeX font package; a Debian binary deployment also needs `lmodern` registered with
fontconfig, not merely `fonts-lmodern` or a generic TeX installation.

1. Check out the desired release tag and copy `.env.example` to `.env` (`chmod 600`).
   Set a newly generated `BEARER_TOKEN` (`openssl rand -hex 32`), `PUBLIC_BASE_URL`
   and an existing dedicated PostgreSQL database/role in `DATABASE_URL`. A
   `replace-` owner token is rejected at startup. There is no unauthenticated
   first-owner registration. Keep this key private and enter it in the web login.
2. Grant the database role ownership of Attic's dedicated database/schema so it
   can apply migrations. Do not use a PostgreSQL superuser. Restrict CONNECT to
   other applications' databases using your existing PostgreSQL access policy.
3. Leave `AI_PROVIDER=chatgpt`; no OAuth credentials are needed to start. Leave
   SMTP host/username/password/sender/recipient empty for browser configuration.
   The base file creates no PostgreSQL or Meilisearch service. Optional search uses
   `SEARCH_URL`, `SEARCH_API_KEY` and `SEARCH_INDEX=attic`; use an index-scoped key
   with the operations listed in [search.md](search.md). Never give Attic the
   shared server's master key. Search outages do not disable canonical content.
4. Build the versioned checkout with `docker compose build attic`, then run
   `docker compose up -d`. Alternatively use `ATTIC_IMAGE` with the immutable
   version tag published by the repository's OCI workflow and run
   `docker compose up -d --no-build`. The GitHub workflow publishes to
   `ghcr.io/<repository-owner>/<repository-name>:<version-tag>` only after runtime
   verification succeeds; verify that a release exists before selecting it.
5. The default binding is loopback port 8080. Put a trusted HTTPS reverse proxy
   in front; do not expose the owner key over public HTTP. The container runs as
   UID/GID 10001, with read-only root and writable `/data`, `/tmp`, `/dev/shm`.
   Pre-create bind-mounted state as 10001:10001. Named volume `attic-data` is shared
   across Compose project names; override the volume for isolated tests.
6. Open Settings, connect ChatGPT using the displayed verification URL/code, and
   explicitly run Check AI compatibility. This synthetic provider call may consume
   quota. Login expires after 15 minutes. Refresh is automatic; reconnect retains
   old credentials until success and no longer blocks refresh while waiting.
7. Configure mail, run Test connection, then deliberately Send test email if wanted.
   The recipient is shown and checked on the server before submission. The plain
   test message checks SMTP submission; use Send to Kindle on a saved PDF to verify
   actual document receipt. Approve the sender in Amazon's Personal Document Email
   List and use a provider app password if required. A successful SMTP authentication
   or DATA acknowledgement does not prove Kindle receipt. Test sends never retry
   automatically; an ambiguous result requires checking the inbox before another send.

## Configuration precedence and secrets

SMTP deployment configuration is authoritative as one complete group whenever any
of host, username, password, sender or destination is nonempty. These fields are
read-only in Settings, including credential clearing. Update the deployment secret
source and recreate Attic to rotate an externally managed configuration. Blank
example defaults (`SMTP_ENABLED=false`, port/TLS/timeout defaults) alone do not lock
browser setup. Avoid partial deployment SMTP settings: Attic deliberately does not
combine a deployment password with a different Settings username or relay.

Otherwise Settings uses `<ARTIFACT_ROOT>/../auth/mail.json`, owner-only mode 0600,
inside a private directory. Writes are atomic and synced before replacing the file.
Blank password retains the existing password; Clear SMTP credentials clears both
username and password. The server never returns existing passwords. Certificate
verification is mandatory; unauthenticated no-TLS mode exists for local mail catchers
only. Changes apply without Compose recreation. Enabling browser-configured delivery
pauses previously pending deliveries; explicitly choose Send to Kindle again for
those items. Saving configuration creates no delivery jobs. Existing environment
based delivery behavior remains compatible.

ChatGPT tokens stay at `CHATGPT_AUTH_FILE` (default `/data/auth/chatgpt.json`), mode
0600, with atomic rotation and cross-process refresh locking. Disconnect removes
local credentials only; revoke provider access in the ChatGPT account separately.
An in-progress browser authorization survives page reloads but not server restart;
restart authorization after a server restart. Run one Attic application replica
for browser settings and authorization. Do not run CLI login concurrently with a
browser disconnect: CLI remains a separate operator action. Neither tokens nor
passwords are included in setup responses or safe diagnostics.

## Runtime, sandbox and health

`/health/live` means the process responds. `/health/ready` checks PostgreSQL,
migrations and writable artifact storage. Neither checks AI, search, mail or PDF.
Settings provides separate checks; `attic check-runtime` is the CLI equivalent of
its runtime check and requires no database credentials. It uses Attic's real offline
saved-page renderer (DOM plus screenshot, sandbox retained) and its actual PDF
template and quality validator. Raw Chromium, TeX and provider responses stay private.

On AppArmor 4 hosts that block nested user namespaces under docker-default, review
and load `deploy/apparmor/attic-browser` using `apparmor_parser -r`, and add
`-f compose.apparmor.yaml` to Compose commands. This profile derives from Docker
26.1.5's default deny rules and adds only the user namespace permission; it is
scoped to Attic. A separately confined dockerd may require its own receive-signal
peer rule. Older AppArmor parsers may not support `userns`; do not apply the file
blindly. This profile still needs validation on the operator's target host.

Run `docker compose run --rm --no-deps attic check-runtime` with the same security
options as the server. A browser failure directs the owner to the scoped namespace
policy or runtime permissions. Do not use privileged mode, `--no-sandbox`, or disable
AppArmor/user-namespace policy host-wide. The Compose capability requirements
SYS_ADMIN/SYS_CHROOT are inherited from Attic's existing sandbox configuration.

## Recovery and upgrades

Canonical state is PostgreSQL plus the entire `/data` volume and deployment
configuration. Secure backups include OAuth tokens and UI SMTP passwords. Portable
archive exports do not include credentials or complete job state. Encrypt backups
and restrict access. Meilisearch index data is disposable for this deployment;
never remove shared search directories or another application's indexes.

Before upgrading, stop canonical writers and back up the dedicated database,
application files and configuration as a consistent set. Test restore into an
isolated database/volume. Migration 0011 adds durable enrichment error codes and
paused-delivery state; it does not alter existing documents. Start the new image
against the restored copy first, verify schema readiness and download a preexisting
artifact with an unchanged checksum. Then exercise capture, actual PDF generation,
search and explicit delivery as appropriate. Preserve the same state volume on
recreation. A binary downgrade alone is not migration-aware recovery: restore the
matching database/files/config snapshot and prior image if rollback is necessary.

Settings → Reindex saved items queues canonical projections and pending deletions.
Use Check indexing progress until pending and failed counts are zero and state is
ready. The adapter waits for each Meilisearch task to apply; the status also executes
a search query. Queue acceptance is not completion. Verify a known saved title by
search before declaring recovery complete. Reindex never deletes whole indexes.

## Validation and homeserver handoff

Deterministic tests cover private settings/rotation/precedence, owner authorization,
concurrent OAuth cancellation and refresh availability, durable stage mapping, SMTP
rejection and uncertain acceptance, and browser transport no-retry behavior. The OCI
workflow executes `check-runtime` with networking disabled, the actual non-root
image, read-only root and sandbox capabilities before publishing. A real public URL
capture remains an opt-in integration (`ATTIC_CHROMIUM_INTEGRATION=1`); no provider
or external email is part of default tests.

This task has not deployed anything or accessed production credentials. The active
Docker context was remote and was not used for builds or mutations. A local Linux
Docker daemon, PostgreSQL integration database and `/usr/bin/chromium` were not
available, so clean image execution, AppArmor loading, database upgrade/persistence
and full browser suites remain operator/CI validation gates. No published image or
successful clean deployment is claimed here.

For the homeserver: keep the existing dedicated database, scoped Meilisearch key,
state volume, proxy and approved sandbox profile. The existing SMTP environment
remains authoritative; remove the entire managed SMTP group only if intentionally
moving it to Settings, and include `auth/mail.json` in protected state backups.
Upgrade the image/binary and bundled migrations together after backup. Connect or
reconnect from Settings instead of docker exec. Check AI and runtime explicitly;
only the owner should opt into an actual email or provider test. Kindle receipt,
physical storage remount and migration rollback still require their own evidence.

### Recorded local checks

- `go test ./...` and `go vet ./...`: passed. Database, real Chromium and LaTeX
  integration cases are opt-in and were skipped without their dependencies.
- `go test -race ./internal/subscription ./internal/setup ./internal/delivery
  ./internal/httpapi`: passed for the settings/concurrency implementation.
- `pnpm build:embed` and `pnpm build:preview`: passed.
- Preview UI suite: 92 passed, one Safari-specific test skipped. An initial run
  accidentally used the production build without a server; it was stopped and
  rerun with the required preview build.
- Production Settings tests (`pnpm build:embed`, then
  `pnpm --filter @attic/ui exec playwright test --config playwright.setup.config.ts`)
  passed on desktop Chromium, mobile Chromium and Safari. They use mocked HTTP only: device-login reload, SMTP save, no-mail connection test
  and explicit recipient-confirmed test send.
- Extension/omnibox Node fixtures passed. `node --test tests/*.test.mjs` also
  attempts two existing tests pinned to `/usr/bin/chromium`; those failed to launch
  on macOS. They are not application assertion failures.

The Settings tests do not prove successful real OAuth, SMTP delivery, Kindle
receipt, image compatibility, AppArmor behavior or database migration recovery.
