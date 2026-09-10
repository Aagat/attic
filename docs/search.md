# Search index

Attic uses Meilisearch for keyword retrieval. PostgreSQL and saved content remain canonical; the index is a disposable projection. An index outage must leave canonical saves and delivery working. The indexing worker must retain durable dirty state until `Upsert` or `Delete` returns nil, retry errors, and serialize mutations for each item so an older projection cannot replace a newer one. A timed-out task may still succeed later; mutations are idempotent.

The `internal/search` module owns index initialization, searchable/filterable attributes, task polling, safe errors and ranked snippets. Its interface has only `Upsert`, `Delete` and `Search`, using typed documents, queries and results. Future semantic/hybrid retrieval can change this adapter without moving canonical state into the index. No embedding provider or speculative adapter is configured.

Use `compose.search.yaml` alongside the main Compose file to run pinned Meilisearch v1.37.0. Put a random `MEILI_MASTER_KEY` (at least 16 bytes) in the ignored `.env`. The override supplies `SEARCH_URL`, `SEARCH_API_KEY` and `SEARCH_INDEX` to Attic and publishes no search port. External Meilisearch installations use the same application variables. Use a credential with index, settings and task access; this adapter creates/configures its index lazily. The key stays server-side.

Search covers titles, tags, notes, URL/domain, captured/PDF text, kind and capture status. Filters are combined with AND; multiple tags require all tags. Saved-date bounds are inclusive at second resolution. Results retain engine relevance order, use offset/limit pagination (HTTP API default 50, adapter default 20, maximum 100), and report estimated totals explicitly. The configured result window is 100,000 items. Snippets are plain text: render using text nodes, never as HTML. Full captured text is indexed but not returned in search hits. Initialization and each mutation share a deadline: the running application configures 15 seconds, while the adapter defaults to 30 seconds when constructed without a timeout; an unfinished engine task does not count as acknowledged indexing.

Index contents are rebuildable rather than a backup substitute. After restoring canonical records/files or replacing the search volume, enqueue every canonical item for indexing and outstanding deletion tombstones for deletion. Search initialization only creates the empty index; it cannot reconstruct records without that worker. Keep the pinned image and its data format together when backing up index data; upgrade explicitly.

## Verification

Hermetic tests exercise completed/failed/stalled tasks, safe errors, query validation and filter escaping. The opt-in real server test exercises index creation/settings, body-only search, matching body and notes snippets, filters, pagination, replacement and repeated deletion:

```sh
ATTIC_TEST_MEILI_URL=http://127.0.0.1:7700 \
ATTIC_TEST_MEILI_KEY=your-test-key \
go test ./internal/search -v
```

The test creates a unique index and deletes that test index afterward. Validated against the pinned Docker image. The proposed 10,000-item, one-second target is not yet benchmarked against a representative collection.

On 2026-09-08, the opt-in library integration check indexed 10,000 synthetic article records of approximately 2.5 KB each across ten topics. The first query after indexing, `speculative decoding`, took **13.2 ms** against the pinned local Docker image and returned 20 hits from 1,000 matching records. This passes the one-second baseline on this machine, but the corpus is synthetic and repetitive: it does **not** establish latency for a representative personal collection, cold process startup, or another deployment host.

Reproduce using `ATTIC_LIBRARY_TEST_SEARCH_URL`, `ATTIC_LIBRARY_TEST_SEARCH_KEY` and `ATTIC_LIBRARY_TEST_BENCHMARK=1` with `go test ./internal/library -run TestLibraryIntegrationSearchTenThousand -v`. The test bulk-loads its own disposable index, waits for indexing to finish, then measures the first populated-index query through the production adapter.

Protocol references: [asynchronous tasks](https://www.meilisearch.com/docs/reference/api/async-task-management/list-tasks), [search snippets](https://www.meilisearch.com/docs/capabilities/full_text_search/getting_started/search_with_snippets), [search parameters](https://www.meilisearch.com/docs/reference/api/search/search-with-get).
