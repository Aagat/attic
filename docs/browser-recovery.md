# Browser recovery

Mobile shares, extension URL saves and imported bookmarks enter the same server
capture pipeline. Existing public APIs and archive discovery run first. X status
links use the public oEmbed endpoint; its result is explicitly partial because
full text, media and thread context are not guaranteed. Truncated embeds retain
that text while attempting further recovery.

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
