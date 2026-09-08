# Attic V1 migrations

This directory contains the PostgreSQL 14+ schema for the Attic
implementation. It is intentionally extension-free and contains no search
engine tables, indexes, or outbox. `content_documents.plain_text` and the
normalized metadata are sufficient for a later full rebuild of a search index.

## Version and runner contract

- Files use a zero-padded numeric prefix: `NNNN_name.up.sql` and
  `NNNN_name.down.sql`.
- The migration runner sorts by the numeric prefix and records applied version
  numbers in its own `schema_migrations` table. That bookkeeping table is not
  part of this domain migration.
- The runner must execute each file as one transaction and must stop startup on
  any error. It must not mark a version applied until the transaction commits.
- Before running an up or down migration, the runner takes the fixed PostgreSQL
  transaction advisory lock `pg_advisory_xact_lock(874611204)`. This uses a
  built-in PostgreSQL primitive and requires no extension. A process crash
  releases the lock automatically.
- Concurrent startup is therefore serialized. The runner must run the lock and
  migration bookkeeping on the same database connection and transaction.
- `SET LOCAL TIME ZONE 'UTC'` is used by the migrations. All temporal columns
  are `timestamptz`; PostgreSQL stores those instants in UTC and the application
  must read/write UTC instants.

## Schema choices

Migration `0002` expands the AI-attempt error-category check without rewriting
or dropping attempt rows. Its down migration preserves rows and maps only the
new categories to the conservative V1 `ai_invalid_response` category.

`jobs` is the durable state machine. `status` and `stage` use text checks rather
than PostgreSQL enums so future input/output adapters can add values through a
normal migration without changing an enum type. The constraints enforce that
only `processing` has a stage and that active processing/delivery has a lease.

IDs are opaque `text` values with bounded lengths. UUID strings are valid, but
the schema does not require an extension or a UUID generation strategy. The
application owns ID generation and correlation IDs.

The source and output records have a stable `*_kind` plus JSON source payload or
profile. V1 accepts URL/PDF, while future file, EPUB, HTML, or other adapters
can be added without making the job table URL-only. `submitted_url` remains a
required projection for URL jobs.

`content_documents` has one immutable document per job. A database trigger
rejects updates; job deletion still removes the document through its foreign
key. `artifacts` uses a generic output kind/profile and stores only a
storage-relative path, safe filename, media type, byte size, checksum, and
availability state. Absolute host paths and secrets are not represented.

`ai_attempts` and `delivery_attempts` contain operational metadata only. AI
attempts use the provider-safe result statuses `succeeded`,
`malformed_response`, `rejected`, and `failed`. Their error check permits the
AI timeout/cancellation, availability, authentication, model, rate-limit,
provider-rejection, response-size, and invalid-response codes, plus the safe
page-decision categories recorded for rejected pages. A successful AI attempt
must have no error code; every other result must have one. These rows never
store prompts, article content, screenshots, raw model responses, SMTP
payloads, or credentials. `deletion_tasks` retains the relative artifact path
so a failed filesystem deletion can be retried even after the job and artifact
rows have been removed.

## Deletion and down-migration semantics

The application should insert a deletion task before deleting job-owned rows.
The `job_id` and `artifact_id` foreign keys use `ON DELETE SET NULL`, preserving
the task and its path. Job-owned idempotency, content, artifact, AI, and
delivery rows use `ON DELETE CASCADE` as required by V1 deletion semantics.

The down migration is intentionally destructive and should be restricted to a
planned rollback or data reset. It drops database records only; the operator
must handle the persistent artifact volume separately and explicitly.

## Indexes

- `jobs_created_keyset_idx` supports reverse-creation list pagination with an
  opaque `(created_at, id)` cursor.
- `jobs_claim_idx` and `jobs_lease_expiry_idx` support queued work, abandoned
  lease reclamation, and bounded worker claims.
- `content_documents_created_keyset_idx` supports a later full catalog/index
  rebuild without adding a search-specific schema now.
- Attempt and deletion indexes support job inspection and retry workers.
