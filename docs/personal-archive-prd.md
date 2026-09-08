# Attic: personal archive and Kindle library

**Status:** Draft for review · **Date:** 2026-09-08

## Purpose

Make Attic the permanent home for everything I bookmark: save it quickly, keep a
usable copy when the original disappears, find it later, and send reading material
to my Kindle Scribe. Kindle delivery remains a primary workflow.

## Starting point

The [original Attic](https://github.com/aagat/attic/tree/1c0dc1e3f262673e7709e90515532c580d18e043)
provides bookmark CRUD, titles/descriptions/tags, URL deduplication, browser HTML
bookmark import, metadata fetching, and a Bleve search interface. Its `archived`
flag does not have an accompanying snapshot/replay implementation in the inspected
source. Relevant code: [bookmarks](https://github.com/aagat/attic/blob/1c0dc1e3f262673e7709e90515532c580d18e043/models/bookmark.go),
[import](https://github.com/aagat/attic/blob/1c0dc1e3f262673e7709e90515532c580d18e043/helpers/importer.go),
[fetching](https://github.com/aagat/attic/blob/1c0dc1e3f262673e7709e90515532c580d18e043/fetcher/fetcher.go),
[search](https://github.com/aagat/attic/blob/1c0dc1e3f262673e7709e90515532c580d18e043/search/mapping.go).

Current Attic adds browser capture, archive-source recovery, approved article text,
Scribe PDFs, SMTP delivery, an extension and mobile sharing. Its library is organized
around processing jobs; search filters only titles/domains already loaded in the browser.

## First-release requirements

1. **Keep every bookmark.** Persist the URL immediately, even for non-articles,
   blocked pages or failed processing. Support editable titles, notes/descriptions
   and tags. Repeated saves find the existing bookmark without discarding annotations
   or automatically resending it. Preserve meaningful query parameters when deduplicating.

2. **Import my collection.** Import browser bookmark HTML and the original Attic
   SQLite database. Preserve available titles, URLs, saved dates, descriptions, tags
   and browser folder paths. Report imported, merged and skipped entries with reasons;
   interrupted imports can resume. Importing never automatically emails the collection.

3. **Keep a local copy.** Capture rendered HTML, required images/styles and searchable
   text in persistent storage, independently of AI approval or PDF success. A saved
   copy should open without contacting the original site or running its scripts.
   Record capture time, original/retrieved URLs and missing resources. Mark partial or
   blocked captures honestly; a screenshot or paywall shell is not a complete archive.
   Use existing archive-source recovery when needed and record that provenance.

4. **Find anything saved.** Search the entire collection server-side across titles,
   URLs, notes, tags, metadata and captured text. Return ranked results with matching
   snippets, domain and saved date; filter by tags, domain, date and capture status.
   Uncaptured bookmarks remain searchable by their saved metadata. Search must work
   without an AI connection and remain rebuildable from stored records and captures.

5. **Read and send to Kindle.** Provide clear actions to open the original, view the
   saved page, read the PDF, and send to Kindle. Preserve current Scribe typography,
   quality checks and durable SMTP delivery. Show capture, PDF and delivery outcomes
   separately. Retrying one outcome must not redo successful work or erase saved content.

6. **Own the archive.** Allow manual recapture while retaining previous successful
   versions; failed recapture never replaces a good copy. Export bookmarks and saved
   content in documented, portable formats. Provide a tested backup/restore path and
   explicit deletion of a bookmark with its captures, PDFs and search entries.

## Product model and delivery order

A **bookmark** is the lasting record. A **capture** is a dated local copy. A **job**
is an attempt to capture, prepare or deliver it. PDFs and email attempts belong to
that record. Adopt existing articles/PDFs into this model without refetching or resending.

Deliver in three slices: bookmark persistence and imports; durable captures and
full-collection search; unified archive/reader/Kindle actions plus export and restore.
Keep the existing Kindle workflow usable throughout. Reuse current modules where
useful; do not revive old runtime code or add legacy compatibility routes.

## Acceptance checks

- Reimporting the same collection creates no duplicate bookmarks or unsolicited emails.
- With the source unavailable, a successful capture still opens with its saved images;
  a phrase found only in its body returns it in search.
- AI, capture, PDF and SMTP failures leave the bookmark and successful outputs intact.
- Restoring a backup recovers bookmarks, annotations and artifacts; search can be rebuilt.
- Proposed performance target: first search results within one second for 10,000 bookmarks
  on the deployment host, verified against a representative collection.

## Decisions for review

- **Quick-save default:** propose “Save and archive,” with a prominent “Save and send
  to Kindle” action and a configurable default for the extension/mobile workflow.
- **Archive fidelity:** propose self-contained static page captures first. Interactive
  application replay, recursive site crawling, video archives and logged-in browser
  session capture are outside this release.
- **Retention:** propose keeping all successful manual captures initially, with visible
  storage usage. Scheduled recapture, automatic pruning, semantic/AI search and
  multi-user sharing can follow later.
