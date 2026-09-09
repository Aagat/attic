# Archive recovery

Attic first tries the submitted page. When recovery is needed, it now prefers a
matching snapshot discovered through the Wayback APIs before trying archive.today
HTML lookup pages:

1. Query the [Wayback Availability API](https://archive.org/help/wayback_api.php)
   for the latest available capture.
2. If Availability fails, returns no matching capture, or points back to the
   submitted replay, query the [CDX API](https://github.com/internetarchive/wayback/tree/master/wayback-cdx-server).
   Request an exact URL match, successful HTML captures, and at most three recent
   results; choose the latest valid result other than the submitted replay.
3. Retain `archive.ph/newest/…` and `archive.is/newest/…` as HTML fallbacks.

Discovery makes at most two API requests with a shared 20-second deadline,
10-second request timeout, 64 KiB response limit, and three-redirect limit.
The existing public-address checks still apply to every request. Returned
snapshots must identify the requested host, escaped path, and query, allowing an
HTTP/HTTPS difference. API discovery does not guarantee the snapshot body is
usable; the normal capture checks still apply. Caller attempt/deadline limits
remain in force, so not every fallback will necessarily be attempted.

Replay URLs containing an explicit original URL can recover through that source
instead of looking for an archive of the archive. This includes Wayback replay
paths and archive.today timestamp/newest paths. Short archive.today IDs cannot
be decoded from the URL alone. Nested archive targets are rejected.

No stable, officially documented archive.today JSON discovery API was verified
as of 2026-09-09; its FAQ was inaccessible during research. Its HTML routes remain
best effort and can still present challenges. Attic does not create public
snapshots or automatically solve CAPTCHAs. Browser-supplied captures remain the
reliable path when the owner can already read the page in their browser.
