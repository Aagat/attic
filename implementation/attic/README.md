# Attic V1 deployable foundation

This directory packages the clean-room Go implementation as a non-root,
read-only-compatible container. It builds `./cmd/attic` into
`/usr/local/bin/attic`. The current composition wires external PostgreSQL,
ordered migrations, durable jobs, filesystem artifacts under `/data`, the
worker, and readiness/HTTP health endpoints; `/tmp` is the ephemeral
workspace.

This is deliberately a **deployable foundation**, not a claim that V1 article
processing is complete. The processor is unavailable, the current image does
not install a headless browser or PDF runtime, the AI adapter is not composed,
and SMTP delivery is not composed. It must therefore not be represented as an
end-to-end extraction or e-reader delivery release. The AI and SMTP
configuration below reserves the V1 deployment contract; configuration alone
does not make those unavailable stages successful.

## Prerequisites

- Linux with Docker Engine and Compose v2 (or another OCI-compatible runtime).
- An existing PostgreSQL 14+ instance reachable through `DATABASE_URL`.
- A persistent volume for `/data`.
- Outbound HTTPS access to submitted sites and the configured AI provider when
  the browser and AI processing stages are composed.
- Outbound SMTP access when the SMTP delivery adapter is composed and enabled.
- HTTPS termination at a trusted reverse proxy or at the application.

The example intentionally has no PostgreSQL container. PostgreSQL is external
and remains the system of record.

## Build

Run these commands from this directory:

```sh
cp .env.example .env
$EDITOR .env
docker build --file Dockerfile --tag attic:foundation .
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
- `AI_BASE_URL`, `AI_API_KEY`, and `AI_MODEL=gpt-5.6-luna` (the AI adapter is
  not composed in this foundation);
- `AI_REASONING_EFFORT=medium`, which may be disabled for providers that reject
  the optional parameter; and
- `SMTP_ENABLED`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS_MODE`, credentials,
  sender, and destination for the reserved delivery configuration. Keep
  `SMTP_ENABLED=false` until the SMTP adapter is composed.

SMTP must use implicit TLS or STARTTLS with certificate verification. Plaintext
SMTP is rejected. When the AI adapter is composed, submitted page content and
screenshots will be sent to the explicitly configured AI provider; review that
data flow before supplying the provider key.

Start the foundation with Compose:

```sh
docker compose -f compose.example.yaml up -d --build
docker compose -f compose.example.yaml logs -f attic
```

The service binds to `127.0.0.1:8080` in the example so a reverse proxy or
private network can provide external HTTPS. The container runs as UID/GID
`10001`, drops Linux capabilities, uses `no-new-privileges`, and has a
read-only root filesystem. Only `/data` and the `/tmp` tmpfs are writable.

## Migrations and readiness

This foundation image packages the migration SQL at the read-only path
`/app/migrations` and includes the health-check contract. The migration runner
consumes that directory and applies ordered migrations against the external
PostgreSQL database while holding the migration lock. The current composition
serves `/health/live` and `/health/ready`; readiness checks configuration,
schema version, and writable artifact storage.

A migration failure stops startup, prevents readiness, and must not create or
initialize a second database.

AI and SMTP reachability are not readiness dependencies because either provider
can be temporarily unavailable; the corresponding adapters are not composed in
this foundation.

Check the container and health endpoints:

```sh
docker compose -f compose.example.yaml ps
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
```

The eventual processing implementation should provide an explicit AI
compatibility-check command. The AI adapter is not composed in this foundation,
so `check-ai` is not currently available. When that command is present, run it
against the configured endpoint without enabling billable processing, for
example:

```sh
docker compose -f compose.example.yaml run --rm attic check-ai
```

This foundation package does not claim that the check or article processing
will succeed before the AI adapter, processor, browser, and PDF stages are
implemented.

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

## Runtime limits and later processing stages

The Compose example gives the process a bounded `/tmp` tmpfs and persistent
`/data`. Durable jobs, PostgreSQL persistence, filesystem artifacts, migration
startup, readiness, health endpoints, and the worker are supplied by the
current application composition. Browser subprocess limits, outbound-address
policy, screenshot/DOM limits, PDF processing, mandatory AI analysis, and SMTP
delivery are not available in this foundation because the processor, browser/
PDF runtime, AI adapter, and SMTP adapter are not composed. When those stages
land, add their runtime dependencies without changing the external PostgreSQL
and artifact-volume contract.
