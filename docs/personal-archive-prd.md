# Attic: things I might need later

**Status:** Scope decisions incorporated; draft for review · **Date:** 2026-09-08

## Product vision

> attic is where I store things that I might need later

Saving should be easy even when I do not yet know how I will use something.
Attic should preserve what I save and help me find and use it when that need arises.
Organization is optional at save time; retrieval should not depend on remembering
where I put something or the exact words I used to describe it.

This release covers web links and uploaded PDFs. Save quickly, preserve a usable
copy, find it later, and read or send it to Kindle Scribe. Kindle delivery remains
a primary workflow.

## Starting point

The [original Attic](https://github.com/aagat/attic/tree/1c0dc1e3f262673e7709e90515532c580d18e043)
provided bookmark editing, tags, browser HTML import, metadata fetching and a Bleve
search interface. Its source includes an `archived` flag but no saved-page replay.
Current Attic provides browser capture, source recovery, approved text, Scribe PDFs,
SMTP delivery, an extension and mobile sharing. Its library is job-based and searches
only titles/domains already loaded in the browser.

## First-release requirements

1. **Keep what I save.** Persist web bookmarks immediately, including non-articles,
   blocked pages and failed captures. Accept PDF uploads, preserve the original file,
   and index extractable text. Support editable titles, notes and tags. Repeated saves
   merge without losing annotations or silently resending content. Preserve meaningful
   query parameters when deduplicating.

2. **Expose both actions.** **Bookmark** saves and archives; it does not send to Kindle.
   **Send to Kindle** also saves and archives the item, then prepares and delivers its
   reading document. Expose both in the extension and web/mobile saving flows. Reuse
   stored documents when available. An archive failure must not prevent delivery of an
   otherwise valid reading document, or erase the saved item.

3. **Keep using browser bookmarks normally.** After enabling browser-bookmark access,
   the Chromium extension reads existing bookmarks and automatically ingests new ones.
   Preserve available titles, dates and folder paths; reconcile edits and moves, and
   catch up after the browser or server has been offline. Browser bookmarking is an
   additional save-and-archive path and never triggers Kindle delivery. No manual export
   is required. Keep HTML bookmark import for other browsers and external collections,
   with resumable processing and imported/merged/skipped counts. There is no old Attic
   database to migrate. This does not imply direct access to mobile Safari's bookmarks.
   [Browser capability](https://developer.chrome.com/docs/extensions/reference/api/bookmarks)

4. **Keep a usable local copy.** Preserve rendered HTML, required images/styles and
   searchable text independently of AI approval or PDF success. Provide a static saved
   page and, for suitable articles, a cleaned reading version. Saved pages must open
   without the original site or its scripts; preserve images, tables and code. Record
   capture time, original/retrieved URLs, archive-source provenance and missing resources.
   Mark partial and blocked captures honestly. Use existing archive-source recovery.

5. **Find and organize automatically.** Search the whole collection across titles,
   URLs, notes, tags, metadata and captured/document text, with ranked results, matching
   snippets and filters for domain, tags, date and capture status. Use AI classification
   and suggested tags when useful; organization must not rely solely on source metadata
   or manual effort. AI enrichment is editable and must not overwrite user choices.
   Saving and keyword search continue when AI is unavailable.

6. **Read, preserve and deliver reliably.** Keep Scribe PDF quality and durable SMTP
   delivery. Show capture, indexing, PDF and delivery outcomes separately; retry only
   failed work. Retain successful captures indefinitely, allow manual recapture with
   version history, and never replace a good capture with a failed one. Show storage
   usage. Export bookmarks and content in documented portable formats; support tested
   backup/restore and explicit removal of an item and its derived data.

## Product model and search design

A **saved item** is the lasting record: a web bookmark or an uploaded PDF. A **capture**
is a dated local copy of a web page. Jobs prepare captures, enrichment, indexes and
reading documents; delivery attempts send documents. Adopt current articles and PDFs
without refetching or resending them.

Design search around an external index, with [Meilisearch](https://www.meilisearch.com/)
or a vector-capable alternative to be selected during technical design. Keyword search
is the first baseline; the design must accommodate semantic or hybrid retrieval later.
Keep canonical records and captured content outside the index so it can be rebuilt.
Indexing is asynchronous and retryable: an index outage must not block saving,
archiving or Kindle delivery. Show pending indexing rather than silently losing items.

## Delivery order and acceptance

Deliver saved-item persistence, browser ingestion and PDF uploads first; then durable
captures, AI enrichment and full-collection search; then complete archive/reader/Kindle
flows and export/restore. Keep Kindle usable throughout. Reuse current modules without
reviving old runtime code or building legacy compatibility routes.

- Existing browser bookmarks appear without export; additions and reconnects converge
  without duplicates, and browser-originated saves send no email.
- Deleting a browser bookmark leaves its Attic copy intact. Explicit deletion in Attic
  removes the item and its derived data; routine browser reconciliation does not restore it.
- Both actions are available: bookmarking archives only; sending also bookmarks and
  archives, with one requested delivery despite ingestion retries.
- With the original site unavailable, a successful saved page still opens with images;
  a phrase found only in captured text or an uploaded PDF finds the item.
- AI or index downtime does not lose saved items. Enrichment can be corrected, and
  restoring a backup recovers records/files and permits rebuilding the index.
- Proposed search target: results within one second for 10,000 saved items on the
  deployment host, measured against a representative collection.

## Scope limits and deletion policy

Interactive application replay, recursive crawling, video archiving, logged-in session
capture, scheduled recapture, automatic pruning and multi-user sharing are deferred.
Image-only PDFs remain preserved even if OCR is deferred; do not imply their text is indexed.

**Deletion policy:** only explicit removal in Attic deletes a saved item. Removing a
browser bookmark leaves its Attic copy, captures and documents intact. Browser folder/title changes update source metadata without
replacing Attic edits. Attic does not rewrite browser bookmarks; deleting an Attic item
should not cause unchanged browser bookmarks to immediately reimport it.
