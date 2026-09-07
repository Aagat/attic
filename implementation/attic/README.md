# Attic V1

This directory packages the clean-room Go implementation as a non-root,
read-only-compatible container. It builds `./cmd/attic` into
`/usr/local/bin/attic`. The current composition wires external PostgreSQL,
ordered migrations, durable jobs, filesystem artifacts under `/data`, the
worker, browser rendering, deterministic extraction, mandatory durable AI
approval, PDF formatting, and readiness/HTTP health endpoints. `/tmp` is the
bounded ephemeral workspace. SMTP delivery remains deferred; completed PDFs
end in `ready` and are available through the authenticated artifact endpoint.

## Prerequisites

- Linux with Docker Engine and Compose v2 (or another OCI-compatible runtime).
- An existing PostgreSQL 14+ instance reachable through `DATABASE_URL`.
- A persistent volume for `/data`.
- Outbound HTTPS access to submitted sites and the configured AI provider.
- HTTPS termination at a trusted reverse proxy or at the application.

The example intentionally has no PostgreSQL container. PostgreSQL is external
and remains the system of record.

## One-command launch

From the repository root, run:

```sh
./launch.sh
```

The launcher verifies Docker Compose, securely asks for the PostgreSQL URL,
reuses `OPENAI_API_KEY` from the ignored root `.env` when available, generates
an owner bearer token, builds the image, starts Attic, and waits for database-
backed readiness. It writes the resulting configuration to the ignored,
owner-readable `implementation/attic/.env`; rerunning the command preserves
the existing values. If PostgreSQL runs directly on the Docker host, use
`host.docker.internal` as the URL hostname.

## Build

### ChatGPT subscription login

Attic can call the ChatGPT Codex backend directly using subscription OAuth.
It does not invoke Codex, OpenCode, or another agent process. This integration
follows the observed third-party protocol described in
[the research note](../../docs/chatgpt-subscription-access.md); it is not the
public Platform API, and available models and usage limits depend on your
ChatGPT account.

Set these values in `.env`:

```dotenv
AI_PROVIDER=chatgpt
CHATGPT_AUTH_FILE=/data/auth/chatgpt.json
AI_MODEL=gpt-5.6-luna
```

`AI_API_KEY` and `AI_BASE_URL` are unused in this mode. Keep `AI_MODEL` set to a
model available to your subscription. To use the existing API-key provider,
set `AI_PROVIDER=api` and configure those API fields.

Build the image and start the native device login command:

```sh
docker compose -f compose.example.yaml build attic
docker compose -f compose.example.yaml run --rm --no-deps attic login-chatgpt
```

Open the printed OpenAI verification URL, enter the displayed code, and sign
in. Device authorization may need enabling in your ChatGPT account security
settings or workspace permissions. Login expires after 15 minutes and can be
cancelled with Ctrl-C. It requires no database or API-key configuration.

The command saves Attic's own tokens under `/data/auth` with owner-only file
permissions. The worker refreshes them automatically, serializes refresh across
processes, and atomically saves rotated tokens. It never imports another
application's credentials. Protect backups of `/data`, which now include these
credentials. If authentication is revoked, rerun `login-chatgpt`.

```sh
docker compose -f compose.example.yaml run --rm --no-deps attic check-ai
docker compose -f compose.example.yaml up -d
```

For the local validation deployment below, add
`-p attic-validation -f compose.validation.yaml` after the base `-f` option in
each command. Login and the worker must use the same project and data volume.
Readiness does not require a logged-in AI provider; `check-ai` explicitly
verifies credentials, image input, and the article-response protocol.

### Local validation deployment

`compose.validation.yaml` adds an isolated PostgreSQL container and publishes
Attic at `http://127.0.0.1:18080`, for machines where port 8080 is occupied.
Use it with the base Compose file, from this directory:

```sh
docker compose -p attic-validation -f compose.example.yaml -f compose.validation.yaml up -d --build
curl -fsS http://127.0.0.1:18080/health/ready
docker compose -p attic-validation -f compose.example.yaml -f compose.validation.yaml run --rm attic check-ai
```

Before starting, configure the ignored `.env` with the normal AI and bearer
settings, `PUBLIC_BASE_URL=http://127.0.0.1:18080`, a random
`VALIDATION_DB_PASSWORD`, and a matching
`DATABASE_URL=postgres://attic:<password>@validation-db:5432/attic?sslmode=disable`.
Use a URL-safe password, such as a randomly generated hexadecimal string.
PostgreSQL is reachable only on the Compose network and retains its data in
`attic-validation-postgres`. This local override is separate from the supported
external-PostgreSQL deployment. Use the same two Compose files and project name
for logs, updates, and shutdown. Do not use the root launcher for this override.

A successful readiness response verifies local dependencies; it does not prove
AI access. If `check-ai` fails because the provider account has no credit,
fund the account or update the AI credentials before attempting an article.
After changing `.env`, rerun `up -d` to recreate the application with the new
configuration. A successful article must reach `ready` and yield a PDF through
`GET /api/v1/jobs/{id}/artifact` with the owner bearer token.

### Build the image directly

Run these commands from this directory:

```sh
cp .env.example .env
$EDITOR .env
docker build --file Dockerfile --tag attic:latest .
```

The multistage build downloads Go modules in the builder, compiles a static
Linux binary, and copies only the clean-room command, internal packages, and
migrations into the image. The runtime stage places the migration SQL at
`/app/migrations` and removes its write permissions; the migration runner
consumes that directory. It does not copy repository-level source, prototype
code, or tests.

The base image tags are intentionally pinned (`golang:1.24.6-alpine3.22` and
`alpine:3.22.1`). Update them as a deliberate image-maintenance change.

## Configure and start

Copy `.env.example` to `.env`, then replace every `replace-` value. Never
commit `.env`, put credentials in command-line arguments, or bake secrets into
the image. Use mounted secret files when supported by the deployment.

Important settings include:

- `DATABASE_URL`: the existing PostgreSQL 14+ connection string;
- `BEARER_TOKEN`: the one-owner bearer credential;
- `ARTIFACT_ROOT=/data/artifacts`;
- `AI_BASE_URL`, `AI_API_KEY`, and `AI_MODEL=gpt-5.6-luna`; `OPENAI_API_KEY`
  is accepted only as a compatibility alias when `AI_API_KEY` is empty;
- `AI_REASONING_EFFORT=medium`, which may be disabled for providers that reject
  the optional parameter; and
- `SMTP_ENABLED`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS_MODE`, credentials,
  sender, and destination for the reserved delivery configuration. Keep
  `SMTP_ENABLED=false` until the SMTP adapter is composed.

SMTP must use implicit TLS or STARTTLS with certificate verification when its
deferred adapter lands. Submitted page content and bounded screenshots are sent
to the explicitly configured AI provider; review that data flow before
supplying the provider key.

Start Attic with Compose:

```sh
docker compose -f compose.example.yaml up -d --build
docker compose -f compose.example.yaml logs -f attic
```

The service binds to `127.0.0.1:8080` in the example so a reverse proxy or
private network can provide external HTTPS. The container runs as UID/GID
`10001`, drops Linux capabilities except `SYS_ADMIN` and `SYS_CHROOT`, which
Chromium needs while establishing its namespace/setuid sandbox, and has a
read-only root filesystem. Do not enable `no-new-privileges` or pass Chromium
`--no-sandbox`. Only `/data`, `/tmp`, and `/dev/shm` are writable.
Chromium and Unicode fonts are installed in the runtime image; browser/PDF
subprocesses run with Chromium's process sandbox enabled as the same non-root
user under CPU, memory, and PID limits.

## Migrations and readiness

The image packages the migration SQL at the read-only path
`/app/migrations` and includes the health-check contract. The migration runner
consumes that directory and applies ordered migrations against the external
PostgreSQL database while holding the migration lock. The current composition
serves `/health/live` and `/health/ready`; readiness checks configuration,
schema version, and writable artifact storage.

A migration failure stops startup, prevents readiness, and must not create or
initialize a second database.

AI reachability is not a readiness dependency because provider outages must not
remove the API from service. SMTP remains disabled.

Check the container and health endpoints:

```sh
docker compose -f compose.example.yaml ps
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
```

Run the explicit compatibility check against the configured endpoint before
submitting jobs. It performs a small real provider request and may be billable:

```sh
docker compose -f compose.example.yaml run --rm attic check-ai
```

## Backup and restore

Back up PostgreSQL and the artifact volume from a consistent maintenance
point. The database contains authoritative job/content/artifact metadata; the
volume contains persisted artifact bytes (including PDF bytes when the PDF
stage is available).

For command-line PostgreSQL tools, do not expand the credential-bearing
`DATABASE_URL` into process arguments. Provision or mount an operator-managed
`.pgpass` file with owner-only permissions (`0600`) and use libpq connection
environment variables instead. For example, provision
`secrets/attic.pgpass` out of band with this shape, replacing the final field
without committing it:

```text
db.example.invalid:5432:attic:attic:REPLACE_WITH_DATABASE_PASSWORD
```

The file must be readable only by the account running `pg_dump`/`pg_restore`;
verify its mode before use. The application can continue to receive its
`DATABASE_URL` through `.env`, but the backup commands below never put that
full URL in argv.

Example PostgreSQL backup (run from one shell so the connection environment is
available to both backup and restore commands):

```sh
export PGHOST=db.example.invalid
export PGPORT=5432
export PGDATABASE=attic
export PGUSER=attic
export PGPASSFILE="$PWD/secrets/attic.pgpass"
test -f "$PGPASSFILE"
test "$(stat -c '%a' "$PGPASSFILE")" = 600

mkdir -p backups
pg_dump --format=custom --file=backups/attic-$(date +%Y%m%d-%H%M%S).dump
```

Example artifact-volume backup (replace the archive name as needed):

```sh
mkdir -p backups
docker run --rm \
  -v attic-data:/data:ro \
  -v "$PWD/backups:/backup" \
  alpine:3.22.1 \
  tar -C /data -czf /backup/attic-artifacts.tar.gz .
```

To restore, stop Attic, restore the PostgreSQL dump and the artifact volume
from the same backup point, then start the container again:

```sh
docker compose -f compose.example.yaml down
pg_restore --clean --if-exists --dbname="$PGDATABASE" backups/attic-YYYYMMDD-HHMMSS.dump
docker run --rm \
  -v attic-data:/data \
  -v "$PWD/backups:/backup" \
  alpine:3.22.1 \
  tar -C /data -xzf /backup/attic-artifacts.tar.gz
docker compose -f compose.example.yaml up -d
```

After restore, validate checksums and readiness, and verify artifact retrieval
when an artifact is available. Do not restore only one side of the
database/artifact pair unless you intentionally accept missing artifacts.

## Upgrade and rollback

1. Take a PostgreSQL and artifact-volume backup.
2. Build or pull the new image under a new immutable tag.
3. Review its migration and compatibility notes.
4. Replace the image and run `docker compose up -d`.
5. Confirm migration completion, readiness, worker startup, and artifact
   storage access.

The migration runner keeps migrations ordered, transaction-safe where
PostgreSQL permits, and protected by a startup lock. Do not run an older binary
against a schema it does not support. If the new migration is
backward-compatible, a binary-only rollback is:

```sh
docker tag attic:foundation attic:foundation.previous
# Retag/build the previously verified image as attic:foundation, then:
docker compose -f compose.example.yaml up -d
```

If the schema is not backward-compatible, stop the service and restore both
PostgreSQL and `/data` from the pre-upgrade backup before starting the previous
image. Keep the previous image tag until the upgrade has been verified.

## Browser acquisition boundary

`internal/acquisition.NewChromiumRenderer` creates a fresh Chromium process and
temporary profile for every render. It captures a configured-size viewport
screenshot and a rendered DOM only after enforcing byte, node, request,
redirect, transfer, navigation, and total-render limits. Cancellation tears
down the process through the executable allocator and waits for cleanup.

Chromium sends HTTP and HTTPS through a loopback policy proxy. The proxy
resolves every hostname, rejects the entire answer set if any address is
non-public, and connects to a validated IP rather than resolving again. CDP
request interception separately rejects non-HTTP(S) requests; downloads and
popups are denied, QUIC is disabled, and non-proxied WebRTC UDP is disabled.
The static `Fetcher` uses the same resolve-validate-connect rule and has no
public arbitrary-transport escape hatch.

There is one unresolved defense-in-depth gap: the renderer does not create its
own kernel network namespace. Its outbound guarantee depends on Chromium
honoring the configured proxy and feature-disable flags. A future browser
feature that bypasses both would not be stopped inside this Go package. Run the
container with a default-deny egress policy or an outbound gateway that permits
only the loopback policy proxy for kernel-enforced isolation. Container CPU,
memory, PID, and filesystem limits are likewise still deployment controls;
deadlines and output limits do not replace them.

The deterministic suite does not start a browser or require network access.
An explicit host integration check uses installed Chromium and public
`example.com`:

```sh
ATTIC_CHROMIUM_INTEGRATION=1 go test ./internal/acquisition -run TestChromiumRendererIntegration
```

## Runtime limits and later processing stages

The Compose example gives the process a bounded `/tmp` tmpfs and persistent
`/data`. Durable jobs, PostgreSQL persistence, filesystem artifacts, migration
startup, readiness, health endpoints, and the worker are supplied by the
current application composition. Browser subprocess, outbound-address,
screenshot, DOM, AI input, and PDF output limits are validated at startup and
enforced by their owning modules. SMTP delivery is the remaining deferred
pipeline stage and does not change the PostgreSQL or artifact-volume contract.
