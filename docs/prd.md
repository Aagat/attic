# Attic V1 Product Requirements Document

Status: Final draft for clean-room implementation
Version: 1.1
Date: 2026-08-31

## 1. Release objective

Attic V1 is a private, headless, self-hosted service that turns public web
articles into clean e-reader PDFs and sends them to a configured destination.
It must be straightforward to deploy on one home server using an existing
PostgreSQL instance.

The V1 flow is:

> Submit URL -> render page -> extract candidate content -> analyze and clean
> with AI -> generate PDF -> persist result -> deliver by email

AI-assisted extraction is a defining part of the product, not an optional
fallback. Every successful URL job must complete an AI analysis stage. The
service may use deterministic extraction to reduce cost and improve fidelity,
but it must not silently produce a final article when AI analysis is unavailable.

The broader vision is a private personal archive with a job interface, content
browsing, and search. V1 stores the normalized content needed for that future
without implementing the UI or search engine yet.

## 2. Current prototype and clean-room boundary

The repository currently demonstrates these behaviors:

- one synchronous endpoint accepts a URL or file;
- a headless browser and Readability produce an article candidate;
- a screenshot is sent to Gemini when Readability fails;
- extracted content is converted to PDF;
- PDF profiles exist for several e-reader sizes;
- optional delivery uses a Gmail-specific SMTP path; and
- a localhost bookmarklet can submit the active browser URL.

The prototype is not the V1 implementation. It lacks durable jobs, PostgreSQL,
authentication, general SMTP configuration, an OpenAI-compatible AI client,
health checks, bounded retries, deployable packaging, and meaningful coverage
for most modules. Its existing tests pass when dependencies can be downloaded,
but most packages have no substantive tests.

This document is the sole behavioral source for the clean-room build. The
clean-room implementation team must not inspect or reuse prototype source,
tests, prompts, module boundaries, internal names, or dependency choices. The
new implementation must live in implementation directories excluded from the
prototype build and must have its own tests derived from this PRD.

## 3. User and deployment assumptions

V1 serves one owner of one deployment. The owner:

- controls the home server and PostgreSQL database;
- controls the e-reader destination and SMTP sender;
- supplies credentials for an OpenAI-compatible, vision-capable API; and
- can configure a reverse proxy or private network access.

V1 runs as one application deployment. API and worker roles may run in the same
process or container. PostgreSQL is external to the deployment and is the system
of record. Generated PDFs are stored on a persistent local volume.

The supported deployment contract is:

- Linux host with an OCI-compatible container runtime;
- PostgreSQL 14 or newer, reachable through `DATABASE_URL`;
- one persistent application-data volume;
- outbound HTTPS access to submitted sites and the AI provider;
- outbound SMTP access to the configured server; and
- HTTPS termination at the application or a trusted reverse proxy.

## 4. V1 scope

### 4.1 Included

- Authenticated submission of one public HTTP or HTTPS URL.
- Durable asynchronous processing backed by PostgreSQL.
- Listing and inspection of jobs through a headless HTTP API.
- JavaScript-capable browser rendering and deterministic article extraction.
- Mandatory AI classification, completeness analysis, metadata enrichment, and
  content cleanup.
- AI access through a configurable OpenAI-compatible API.
- `gpt-5.6-luna` as the default model identifier.
- PDF generation with one configured e-reader profile.
- Persistent storage and authenticated download of generated PDFs.
- Persistent storage of normalized article content and metadata.
- Email delivery through configurable SMTP.
- Automatic retries for unambiguously transient failures.
- Job resubmission and deletion through the API.
- Database migrations, health checks, container packaging, backup guidance, and
  a documented home-server deployment procedure.

### 4.2 Deferred

- File and image uploads, EPUB input, feeds, and batch submission.
- Browser extensions, bookmarklets, and mobile share integrations.
- A graphical user interface.
- Full-text search endpoints and Meilisearch integration.
- Tags, collections, annotations, highlights, and reading progress.
- Multiple users, destinations, or SMTP accounts.
- Multiple active PDF profiles per deployment.
- OCR-only documents and multi-screenshot reconstruction of very long pages.
- Automatic retrieval from third-party web archives.

### 4.3 Guardrails

Attic V1 processes content available to a new, unauthenticated browser session.
It does not import cookies, execute login flows, bypass bot challenges, defeat
paywalls, or use archives to recover restricted content. It does not republish
stored content or expose it without authentication.

## 5. Product decisions

| Area | V1 decision |
| --- | --- |
| Interface | Headless HTTP JSON API |
| User model | One owner and one bearer token |
| Processing | Durable asynchronous jobs |
| Database | Existing external PostgreSQL |
| Artifact storage | Persistent local volume; metadata in PostgreSQL |
| AI role | Required for every successful URL job |
| AI protocol | OpenAI-compatible Chat Completions subset |
| Default AI model | `gpt-5.6-luna` |
| AI fallback | No provider-specific fallback; retry transient failures |
| Output | PDF |
| Delivery | Configurable SMTP |
| Content retention | Indefinite by default, until owner deletion |
| Raw page retention | Removed after the processing attempt |
| UI and search | Deferred, with stable data and API seams preserved |

The official OpenAI model identifier is `gpt-5.6-luna`. OpenAI documents image
input, structured output, Chat Completions, and Responses support for this model:
<https://developers.openai.com/api/docs/models/gpt-5.6-luna>.

## 6. Core user stories

- As the owner, I can submit an article URL and immediately receive a job ID.
- As the owner, I can list jobs and inspect the current stage or final outcome.
- As the owner, I receive AI-cleaned content rather than page chrome or an
  unverified deterministic extract.
- As the owner, I receive a readable PDF at my configured e-reader destination.
- As the owner, I can download the PDF when delivery fails.
- As the owner, I can resubmit a failed job and can delete a job and its content.
- As the operator, I can deploy and upgrade Attic without manually editing the
  database or installing browser and PDF dependencies on the host.
- As the operator, I can identify failures without exposing page content or
  secrets in logs.

## 7. Domain model and persistence

PostgreSQL is authoritative for job state and normalized content. Every durable
record uses a stable opaque ID and UTC timestamps.

### 7.1 Job

A job represents one submission and contains:

- ID and optional idempotency key;
- submitted URL and resolved canonical URL when known;
- status, current stage, attempt count, and timestamps;
- selected PDF profile;
- failure category, safe message, and correlation ID;
- links to its content document, artifact, AI attempts, and delivery attempts;
  and
- optional `retry_of_job_id` linking a resubmission to the original job.

### 7.2 Content document

A successful extraction creates one content document containing:

- stable content ID and owning job ID;
- title, author, site name, publication date, description, and source URL;
- sanitized semantic HTML used for PDF generation;
- normalized plain text suitable for future indexing;
- extraction method and AI confidence;
- detected language when available; and
- creation and update timestamps.

Content documents are immutable in V1. Resubmitting a URL creates a new job and
new content document rather than modifying history.

### 7.3 Artifact

An artifact records the PDF's storage-relative path, safe filename, media type,
byte size, checksum, profile, creation time, and availability state. Absolute
host paths are never returned by the API or stored in logs.

### 7.4 AI attempt

Each AI request records provider-safe operational metadata:

- job ID, purpose, attempt number, model identifier, prompt version, and latency;
- provider request ID when available;
- input and output token counts when returned by the provider;
- result status and a safe error category; and
- creation timestamp.

API keys, authorization headers, screenshot bytes, full prompts containing page
content, and raw model responses are not stored in this record.

### 7.5 Delivery attempt

A delivery attempt records attempt number, stable message ID, timestamps,
outcome, and a sanitized provider response code. It never stores SMTP secrets or
the full email payload.

### 7.6 Deletion

Deleting a job must delete its content document, AI-attempt metadata, delivery
attempts, artifact record, and artifact file. Deletion completes synchronously
or leaves a durable deletion task that finishes within 24 hours. A failed file
deletion remains visible to the operator and is retried.

Database migrations are ordered, transactional where PostgreSQL permits, and
safe to run once during startup. Concurrent startup is protected by a migration
lock. A failed migration prevents readiness and leaves the previous schema
recoverable.

## 8. Headless API

Every non-health endpoint lives under `/api/v1`, consumes or produces JSON
except artifact download, and requires `Authorization: Bearer <token>`.

### 8.1 Create job

`POST /api/v1/jobs`

Request:

```json
{
  "url": "https://example.com/article",
  "title": "Optional title hint",
  "profile": "optional-configured-profile"
}
```

The endpoint validates the request, durably creates the job, and returns
`202 Accepted` within 2 seconds under normal home-server load.

Response:

```json
{
  "id": "opaque-job-id",
  "status": "queued",
  "created_at": "2026-08-31T12:00:00Z",
  "links": {
    "self": "/api/v1/jobs/opaque-job-id"
  }
}
```

The endpoint accepts an optional `Idempotency-Key` header. The same key and body
within 24 hours return the original job. Reusing the key with a different body
returns `409 Conflict`.

### 8.2 List jobs

`GET /api/v1/jobs?limit=<n>&cursor=<opaque>&status=<optional>` returns jobs in
reverse creation order. `limit` defaults to 25 and is capped at 100. Pagination
uses an opaque stable cursor rather than page offsets. The response includes the
next cursor when more results exist.

Each list item includes job ID, title when known, source host, status, stage,
created time, completed time, failure category, and whether an artifact exists.
The endpoint does not return article bodies.

### 8.3 Inspect job

`GET /api/v1/jobs/{id}` returns:

- job identity, status, stage, attempt count, and timestamps;
- submitted and canonical URLs with configured sensitive query values redacted;
- extracted metadata and AI confidence when available;
- artifact filename, size, checksum, and authenticated download link;
- delivery status and attempt count; and
- failure category, safe message, and correlation ID.

Unknown and deleted IDs return `404 Not Found`.

### 8.4 Download artifact

`GET /api/v1/jobs/{id}/artifact` returns the completed PDF using
`application/pdf`, a sanitized filename, and attachment semantics. It returns
`404` when no complete artifact exists. Delivery failure does not affect
download availability.

### 8.5 Retry job

`POST /api/v1/jobs/{id}/retry` creates and returns a new queued job with the same
URL, title hint, and profile. It is allowed only when the source job is terminal.
The new job links to the original through `retry_of_job_id`.

### 8.6 Delete job

`DELETE /api/v1/jobs/{id}` is idempotent and returns `204 No Content` once the
job is absent or deletion has been durably scheduled. A job being processed is
cancelled before its data is removed.

### 8.7 Health endpoints

- `GET /health/live` reports whether the process can respond.
- `GET /health/ready` succeeds only when configuration is valid, PostgreSQL is
  reachable at the required schema version, and artifact storage is writable.

Health responses expose no configuration values, database details, or secrets.
AI and SMTP reachability do not gate readiness because transient external
outages must not remove the API from service.

### 8.8 Error envelope

Every JSON error uses:

```json
{
  "error": {
    "code": "stable_machine_code",
    "message": "Safe actionable message",
    "correlation_id": "opaque-correlation-id"
  }
}
```

Client responses contain no stack traces, SQL text, raw provider responses,
secrets, local paths, or article content.

## 9. Job lifecycle

### 9.1 Public statuses

| Status | Meaning | Terminal |
| --- | --- | --- |
| `queued` | Accepted and waiting for work | No |
| `processing` | Fetch, deterministic extraction, AI, or PDF work is active | No |
| `delivering` | PDF exists and SMTP delivery is active | No |
| `ready` | PDF exists and configured email delivery is disabled | Yes |
| `delivered` | SMTP server accepted the message | Yes |
| `delivery_failed` | PDF exists but delivery failed or is uncertain | Yes |
| `failed` | No complete PDF was produced | Yes |
| `cancelled` | Owner deletion stopped active work | Yes |

While `processing`, the stage is exactly one of `fetching`, `extracting`,
`ai_analyzing`, `formatting`, or `persisting`. Every status and stage transition
is stored before it is returned.

### 9.2 Recovery and retries

On restart, queued jobs remain queued. Interrupted processing resumes from the
first incomplete durable stage. Complete artifacts are not regenerated. A job
abandoned in an active state is reclaimed after a configurable lease expires.

The service retries unambiguously transient fetch, browser, AI rate-limit, AI
server, and SMTP failures twice after the initial attempt with bounded backoff
and jitter. Validation, policy, authentication, paywall, unsupported-content,
and malformed-AI-response failures are permanent after the rules in section 10
are exhausted.

SMTP is not retried after reported acceptance. If acceptance is uncertain, the
job becomes `delivery_failed` rather than risking duplicate delivery.

## 10. Content processing pipeline

### 10.1 Fetch and render

The worker opens the URL in an isolated headless browser with JavaScript enabled.
It waits only within configured navigation and render deadlines, then captures:

- final URL and response status;
- page title and relevant metadata;
- rendered DOM for deterministic extraction;
- one bounded screenshot for AI analysis; and
- network and rendering diagnostics that contain no response bodies.

The worker blocks downloads, popups, new windows, non-HTTP protocols, and
requests prohibited by section 14. It closes browser processes after success,
failure, timeout, or cancellation.

### 10.2 Deterministic candidate

A deterministic reader algorithm creates a candidate containing semantic HTML,
plain text, title, author, site name, description, publication date, canonical
URL, and image references when available.

The candidate preserves headings, paragraphs, lists, quotes, code blocks, links,
images, captions, and alt text. It removes navigation, advertising, comments,
consent overlays, subscription prompts, share controls, and repeated page chrome.

Deterministic extraction failure does not end the job when a usable screenshot
exists. The AI stage receives an empty candidate and attempts visual extraction.

### 10.3 Mandatory AI stage

Every URL job must invoke the configured AI provider. AI receives:

- a versioned system instruction;
- source URL and deterministic metadata;
- bounded candidate plain text and sanitized semantic HTML when available; and
- the rendered screenshot as an image input.

The instruction asks the model to classify the page, evaluate candidate
completeness, extract or clean the article, and return this logical result:

- classification: `article`, `paywall`, `access_denied`, `error_page`,
  `interactive`, or `non_article`;
- decision: `accept_candidate`, `replace_candidate`, or `reject`;
- cleaned semantic article content when replacing the candidate;
- title, author, site name, publication date, description, and language;
- completeness and confidence values from 0 to 1; and
- a short user-safe decision reason.

The model is not asked for hidden reasoning or chain-of-thought. The response is
validated against an application-owned schema. The application accepts the
deterministic candidate only when AI returns `article` and `accept_candidate`.
It uses AI content only when AI returns `article` and `replace_candidate` with
non-empty, sanitized content above the configured minimum length. Every other
decision fails with the matching category.

Malformed output receives one schema-repair request using the same provider and
model. A second malformed output ends the job with `ai_invalid_response`.
Provider timeouts, rate limits, and server failures follow the job retry policy.
Provider authentication and unsupported-model errors fail immediately.

An AI outage never causes unreviewed deterministic content to be delivered. The
job remains retryable through the API after the provider recovers.

### 10.4 OpenAI-compatible provider contract

V1 depends on this compatibility subset:

- HTTPS base URL configured by `AI_BASE_URL`;
- bearer API key configured by secret `AI_API_KEY`;
- model configured by `AI_MODEL`, defaulting to `gpt-5.6-luna`;
- `POST <base-url>/chat/completions` semantics;
- text and image content parts in a user message;
- non-streaming JSON response containing one assistant text message; and
- standard HTTP status handling for authentication, rate limits, and server
  failures.

V1 must not depend on OpenAI-only hosted tools, file storage, conversations,
background mode, or provider-side response persistence. It requests structured
JSON when the provider supports it, but schema correctness is always validated
locally. The integration test suite includes a fake compatible server and must
not require paid credentials.

Configuration supports model-specific reasoning effort only as an optional raw
string. The default for `gpt-5.6-luna` is `medium`. A compatible provider that
rejects this parameter can disable it through configuration.

### 10.5 AI privacy and observability

The owner explicitly configures the provider and therefore authorizes sending
submitted page content and screenshots to it. Deployment documentation must
make this data flow visible before the owner supplies credentials.

Logs record provider host, model, latency, attempt, provider request ID, token
counts, and error category. Logs omit keys, authorization headers, request and
response bodies, prompts containing article text, screenshots, and extracted
content. The provider base URL is logged without user information or query data.

### 10.6 Final content validation

Before PDF generation, the final article must:

- be classified as an article by AI;
- have a non-empty title;
- contain at least 500 Unicode characters of coherent primary text by default;
- contain only the allowed sanitized semantic HTML subset;
- have absolute HTTP or HTTPS links; and
- contain no scripts, event handlers, forms, embedded active content, or remote
  frames.

AI output is always untrusted input and receives the same sanitization as page
content.

## 11. PDF and artifact requirements

V1 has one active reading profile selected in deployment configuration. The
default is A5 portrait, 10 mm margins, 11 pt serif body text, 1.4 line height,
distinct headings, and no page numbers or running headers.

The PDF includes title, available author/date/site metadata, cleaned article,
useful images within size limits, source URL, and generation timestamp. Text is
selectable Unicode, links are clickable, images fit the content area, and no
active content or embedded files are present.

The filename is derived from the title, restricted to safe characters, limited
to 100 characters before `.pdf`, and falls back to `attic-<job-id>.pdf`.

Artifacts are written atomically: a partial file is never downloadable. The
stored checksum and size are verified after writing. Artifact storage persists
across container replacement and ordinary restart.

## 12. SMTP delivery

SMTP configuration includes host, port, TLS mode, username, password or app
token, sender, and destination. V1 supports implicit TLS and STARTTLS with
certificate verification; plaintext SMTP is rejected.

The message contains a safely encoded article title, a plain-text source URL,
the PDF attachment, and a stable message ID derived from the job. `delivered`
means the SMTP server accepted the message, not that the e-reader displayed it.

Email delivery can be disabled for local validation. When disabled, a complete
PDF ends the job in `ready`; the artifact remains downloadable.

## 13. Failure categories

Every failed terminal job has one primary stable category:

| Category | Meaning | PDF may exist |
| --- | --- | --- |
| `invalid_input` | URL or profile is invalid | No |
| `blocked_target` | Network policy rejected the destination | No |
| `fetch_failed` | Page could not be retrieved | No |
| `render_timeout` | Browser rendering exceeded its deadline | No |
| `access_denied` | Site returned login, challenge, or denial content | No |
| `paywall_detected` | Accessible page appears paywalled | No |
| `unsupported_content` | Page is not a suitable long-form article | No |
| `insufficient_content` | Candidate or AI result is too incomplete | No |
| `ai_unavailable` | Compatible provider failed after transient retries | No |
| `ai_auth_failed` | Provider rejected configured credentials | No |
| `ai_model_unsupported` | Model lacks a required API or vision capability | No |
| `ai_invalid_response` | Provider returned invalid output twice | No |
| `format_failed` | A complete valid PDF could not be generated | No |
| `storage_failed` | Content or artifact could not be committed | Possibly |
| `delivery_rejected` | SMTP server rejected the message | Yes |
| `delivery_timeout` | SMTP result failed or is uncertain | Yes |
| `internal_error` | Unexpected failure; inspect correlation ID | Possibly |

## 14. Security requirements

### 14.1 Inbound access

- Every API endpoint except health requires the configured bearer token.
- Authentication uses constant-time secret comparison and is rate-limited.
- CORS is disabled by default and accepts only configured origins when enabled.
- Production traffic uses HTTPS at the service or trusted reverse proxy.
- Request bodies, headers, URL length, and concurrent requests are bounded.

### 14.2 Outbound requests

The service resolves and validates every destination before connecting. It
rejects loopback, link-local, private, carrier-grade NAT, multicast, reserved,
documentation, benchmarking, and otherwise non-public IPv4 and IPv6 addresses.

Validation applies to initial navigation, every redirect, and every browser
subresource. A connection uses the validated address so a DNS change cannot
redirect it to a prohibited target. Redirects are capped at five. Private-host
exceptions are outside V1.

### 14.3 Secrets and untrusted content

- Database, API, SMTP, and bearer credentials come from environment variables
  or mounted secret files and never from command-line arguments.
- Logs and client errors redact credentials, authorization headers, cookies,
  URL fragments, and query values by default.
- Browser, page HTML, AI output, metadata, filenames, email headers, and PDF
  inputs are all treated as untrusted.
- The browser runs as a non-root user in an isolated environment with the least
  filesystem access required.
- Response bytes, DOM size, screenshot dimensions and bytes, image bytes, PDF
  bytes, subprocess runtime, job duration, queue depth, and concurrency have
  finite configurable limits.

### 14.4 Retention

Normalized content, job records, and complete artifacts are retained until the
owner deletes them. Raw HTML and screenshots exist only during active
processing. Optional failed-job diagnostics are disabled by default; when
enabled, a sanitized snapshot and screenshot expire after 24 hours.

## 15. Deployment requirements

The clean-room implementation is deployable when the repository provides:

- a pinned, reproducible multi-stage container build;
- one runtime image containing the browser and PDF runtime dependencies;
- a non-root runtime user and support for a read-only root filesystem with
  explicit writable data and temporary mounts;
- an example Compose file that connects to an external PostgreSQL service rather
  than creating a second database by default;
- an example environment file containing names and safe placeholders only;
- automatic database migration with failure-safe startup;
- liveness and readiness checks usable by the container runtime;
- graceful shutdown that stops accepting work, releases job leases, closes the
  browser, and exits within a configured deadline;
- persistent-volume documentation for artifacts;
- reverse-proxy and private-network exposure guidance;
- PostgreSQL and artifact-volume backup and restore instructions; and
- an upgrade and rollback procedure that identifies schema compatibility limits.

Required configuration groups:

- server: listen address, public base URL, bearer token, trusted proxy settings;
- database: `DATABASE_URL`, pool bounds, migration behavior;
- storage: artifact root and optional diagnostic retention;
- AI: base URL, key, model, reasoning effort, timeouts, and retry limits;
- browser: deadlines, concurrency, and resource limits;
- PDF: active profile values and output-size limit; and
- SMTP: enabled flag, server, TLS mode, credentials, sender, and destination.

Startup validates required values without performing billable AI work. A
documented operator command performs an explicit AI compatibility check using a
small bundled text-and-image fixture and reports whether the configured endpoint
meets section 10.4.

## 16. Observability

Application logs are structured JSON in production. Every job log includes job
ID, correlation ID, stage, attempt, duration, and failure category where
relevant. Request logs include method, route template, status, and duration, not
raw URLs or bodies.

At minimum, the service exposes authenticated operational metrics or structured
periodic counters for:

- queued and active jobs;
- jobs completed by outcome and failure category;
- stage duration;
- AI request count, latency, errors, and token usage;
- SMTP attempts and outcomes; and
- browser process count and cleanup failures.

Metrics never include full URLs, article titles, article bodies, query values,
or credentials as labels.

## 17. V1 acceptance criteria

### 17.1 Deployment

- On a clean Linux host with a container runtime and reachable PostgreSQL, the
  documented deployment starts without installing application dependencies on
  the host.
- Startup applies migrations, readiness passes, and a restart preserves queued
  jobs, normalized content, and artifacts.
- Replacing the application container while reusing the data volume and database
  preserves all user-visible state.
- Invalid database, storage, AI, or SMTP configuration reports the setting name
  without printing its value.

### 17.2 API and persistence

- A valid URL returns `202` and a durable job ID within 2 seconds under the
  documented single-user load.
- Idempotent duplicate submissions return one job; conflicting reuse returns
  `409`.
- List pagination is stable when new jobs are added between requests.
- Retry creates a distinct linked job; deletion removes database content and the
  artifact within 24 hours.
- Requests without the bearer token cannot create, list, inspect, retry, delete,
  or download.

### 17.3 Extraction and AI

- Every successful URL job contains a successful AI-attempt record with the
  configured model and prompt version.
- A static article and JavaScript-rendered article both produce normalized
  content containing title, main body, and source without navigation or ads.
- AI can accept a complete deterministic candidate and replace an incomplete
  candidate from the screenshot path.
- Provider unavailability never delivers deterministic content without AI
  approval.
- A fake OpenAI-compatible server proves request routing, bearer auth, model
  selection, image input, JSON validation, one repair attempt, timeout, rate
  limit, and server-error handling.
- Setting `AI_MODEL=gpt-5.6-luna` sends that exact model identifier.
- Paywall, access-denied, interactive, non-article, and insufficient pages fail
  with their stable categories and produce no PDF.

### 17.4 PDF and delivery

- The PDF opens in a standards-compliant viewer, contains selectable Unicode
  text, uses the configured profile and safe filename, and contains no active
  content.
- Artifact metadata matches the stored file's checksum and byte size.
- Disabled SMTP produces `ready`; SMTP acceptance produces `delivered`;
  permanent rejection produces `delivery_failed` while leaving the PDF
  downloadable.
- An uncertain SMTP acceptance is not automatically retried.

### 17.5 Security and operations

- Direct, redirected, DNS-rebound, and subresource requests to prohibited
  addresses are blocked before connection.
- Automated tests prove that credentials, authorization headers, cookies, raw
  AI bodies, screenshots, article text, and URL query values do not enter logs or
  client errors.
- Oversized inputs and timed-out jobs terminate within configured bounds and
  leave no browser or PDF subprocess running.
- The explicit AI compatibility check succeeds against the default OpenAI
  endpoint with `gpt-5.6-luna` when valid credentials are provided, and against
  the test fake without external credentials.

## 18. Implementation sequence

### Increment 1: Deployable foundation

- Create the clean-room implementation and exclude prototype code from its build.
- Add validated configuration, PostgreSQL migrations, job/content persistence,
  authentication, health endpoints, artifact storage, and container packaging.
- Implement create, list, inspect, retry, delete, and artifact-download APIs.

Completion criterion: the example deployment connects to an external PostgreSQL
instance, survives a container replacement, and passes every deployment and API
acceptance case using a synthetic worker result.

### Increment 2: Deterministic browser pipeline

- Add isolated rendering, outbound-request policy, deterministic extraction,
  metadata capture, sanitization, limits, and browser cleanup.

Completion criterion: static and JavaScript fixture pages produce sanitized
candidate documents, and every network-policy, timeout, cancellation, and
cleanup acceptance case passes.

### Increment 3: Mandatory compatible AI

- Add the provider-neutral client, versioned prompt, AI-attempt records, local
  output schema, repair path, retry classification, and compatibility command.
- Make successful AI analysis a hard precondition for PDF generation.

Completion criterion: all AI acceptance cases pass against the fake provider,
and an opt-in live smoke test passes with `gpt-5.6-luna` through the official
OpenAI API.

### Increment 4: PDF and delivery

- Add atomic PDF generation, artifact verification, secure SMTP, stable message
  IDs, delivery recovery, and downloadable delivery failures.

Completion criterion: all PDF, artifact, SMTP, restart, and uncertain-delivery
acceptance cases pass end to end.

### Increment 5: Release hardening

- Add the representative article corpus, structured observability, resource and
  concurrency tests, backup/restore docs, and upgrade/rollback docs.

Completion criterion: every criterion in section 17 passes in the documented
home-server deployment.

## 19. Future UI and search seam

V1 deliberately preserves the data needed by later product layers:

- the UI can use the versioned list, detail, retry, delete, and download APIs;
- stable content IDs separate article content from processing jobs;
- plain text and metadata can be backfilled into PostgreSQL full-text search or
  an external engine such as Meilisearch;
- immutable content documents make indexing repeatable; and
- normalized timestamps, source URL, language, title, author, and description
  provide filterable index fields.

V1 does not add a search-engine dependency, search-specific schema, or indexing
outbox before a search implementation is selected. The PostgreSQL content store
must remain sufficient to rebuild a future index from scratch.

## 20. Clean-room completion rule

The release is complete only when a reviewer who did not consult the prototype
can trace every implemented behavior to this PRD, all V1 acceptance criteria
pass, and the deployable artifact contains no prototype source, prompts, tests,
or dependencies copied for behavioral parity.
