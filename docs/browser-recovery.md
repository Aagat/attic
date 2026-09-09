# Browser recovery

Mobile shares, extension URL saves and imported bookmarks enter the same server
capture pipeline. X/Twitter status links first try the allowlisted XCancel mirror.
The main post's own permalink must match the requested post ID. Its full text,
author and date are preserved separately from replies; unverified media/thread
context remains explicitly partial. Direct XCancel status links use this same
parser. Mirror requests have a 10-second timeout, 2 MiB size limit and at most two
redirects, through the existing public-address network policy.

If the mirror is unavailable, Attic tries X's public oEmbed endpoint and retries
the mirror when that response supplies a different canonical username spelling.
It then continues with ordinary
browser/archive recovery. Short embeds no longer stop the search for a fuller
copy. Chromium automatically clicks the requested post's Show more control, at
most three times; a remaining expansion control marks the snapshot as truncated.
The control must belong to the post identified by its timestamp permalink, so
replies and quoted posts are not expanded accidentally. Unavailable mirrors,
truncated embeds and failed expansion do not replace the saved URL or earlier
versions. No mirror account, publisher cookies, or additional API key is needed.

If ordinary capture is blocked or an embed is truncated, Attic opens headed
Chromium on a private virtual display. The configured AI can inspect the viewport
and perform at most four bounded click, text, scroll or wait actions within 90
seconds. Article approval and PDF checks still run separately. No arbitrary
JavaScript, shell commands or model-supplied navigation URLs are exposed.

**Continue capture** in an item's reader opens the same browser in the PWA.
The owner can take control, tap the page, scroll and type into its focused field.
Takeover cancels AI work; subsequent human input is not sent to the model.
A readable, stable page at the original address is saved automatically. After a
navigation to another address, the owner checks the visible URL and explicitly
chooses **Save page**. Challenges and error pages cannot be saved as successful
captures. Closing the dialog closes the browser, preserving its private profile.

One browser can be active at a time, for at most 15 minutes. This bounds memory
and avoids simultaneous writers to a Chromium profile. A busy browser does not
block saving other bookmarks; another item's recovery can be started afterward.
Profiles are stored per source hostname in `/data/browser-profiles` (beside the
artifact root), with owner-only permissions. Cookies can survive graceful close
and application restarts; websites still control their expiry and validity.
Neither profiles nor existing items are deleted by recovery. Browser profiles
contain private browsing state and should be included in private backups, not
shared with exported bookmarks.

The browser uses Attic's public-address network policy and bounded transfer
budget. Chromium's debug connection and virtual display are never published.
Screenshots and input travel through the existing authenticated Attic API, with
no-store responses. Model failures, login requirements and unresolved challenges
hand control to the owner; automatic CAPTCHA success is not promised.

Successful recovery appends a capture and invalidates stale capture work. Failed
PDF/delivery requests are resumed from the preserved content, retaining the old
job and its original delivery intent. The capture/job timestamps and stable
request identity prevent one new capture from causing an infinite retry loop.

The Docker runtime starts Xvfb automatically. Running the Go server outside
Docker requires a working `DISPLAY` for recovery; ordinary isolated headless
capture and PDF rendering remain available without it.

Validation includes provider/action tests, takeover cancellation, document
identity, challenge rejection, database capture-history and retry-race tests,
mobile UI interactions, and headed Chromium cookie persistence. Run the latter
inside the runtime image with `ATTIC_SESSION_INTEGRATION=1` and a virtual display.
