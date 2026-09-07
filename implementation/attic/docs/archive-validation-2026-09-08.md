# Archive recovery validation — 2026-09-08

The production processor now attempts existing archive copies after a recoverable
original-source failure. Recovery uses the same Chromium address policy, AI
approval gate, durable attempt recording and checked PDF formatter.

## Automated evidence

`go test ./...` and `go vet ./...` pass. Recovery tests exercise a paywalled,
access-denied or incomplete original, an unavailable first archive, then a
successful second archive through the real worker and memory store. They verify
that the job becomes ready only after approval, the snapshot source reaches the
formatter, and a second job gets its own complete source budget. Additional tests
cover exhaustion, fatal authentication/storage failures, cancellation, malformed
or mismatched Wayback responses, response-size limits, blocked addresses and
preventing recursive archive lookup. Existing stage-order and unpublished-PDF
quality-failure tests continue passing.

## Live deployment evidence

Submitted the user's Economist URL through the deployed authenticated API:

https://www.economist.com/finance-and-economics/2026/09/04/the-jobs-apocalypse-is-postponed-an-ai-jobs-boom-is-here

Job `175e4cb0ce7a96acc494724be9ee6201` exercised PostgreSQL stage transitions from
AI analysis back to fetching twice. Original access was denied. Both archive.ph
and archive.is attempts also returned access-denied classifications. Wayback's
availability API returned HTTP 200 with an empty `archived_snapshots` object.
The job correctly ended with `access_denied` and no artifact after exhausting
available candidates. This validates live exhaustion, not successful live archive
extraction; successful recovery is covered by the deterministic worker test.

Snapshot discovery follows the providers' documented interfaces:
- https://archive.ph/faq (newest-snapshot links)
- https://archive.org/help/wayback_api.php (availability API)
