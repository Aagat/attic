# Attic — product and UI design brief

Prepared September 8, 2026. This is a standalone brief for a complete UI design of the existing product, including its web app, mobile experience and Chromium extension.

## 1. Product vision

> attic is where I store things that I might need later

Attic is a private personal archive. It lets its owner save something quickly, preserve a usable copy, find it later, and read or use it when needed. The owner should not have to decide where something belongs or why it might matter before saving it.

The current content types are **web links and uploaded PDFs**. A link can be an article, reference page or other useful web page; it does not have to qualify as an article to belong in Attic.

**Sending content to Kindle is a primary capability**, alongside bookmarking and preservation. The owner's primary reading device is a Kindle Scribe. Attic should make moving between discovering something on a phone or computer, finding it in the archive, and reading it on Kindle feel straightforward.

This is a self-hosted, single-owner product. The design does not need team workspaces, social feeds, public profiles, subscriptions or a marketing website.

## 2. Audience and design direction

The primary user saves useful material across a Chromium desktop browser, Android and iOS devices. They already use browser bookmarks and want to keep doing so. They may return to saved material months later, remembering an idea or phrase rather than its title or folder.

Recommended design qualities:

- **Quick to save:** organization and metadata editing are optional.
- **Easy to retrieve:** search and recognizable content matter more than decorative presentation.
- **Trustworthy:** clearly distinguish a saved link from a preserved page and a delivered document.
- **Calm:** a large collection should feel like a useful resource, without pressure to clear a reading backlog.
- **Content-focused:** long titles, technical articles and documents should remain legible.

The current UI is a working starting point. The designer is free to rethink navigation, layout, visual identity, typography and components. The behavior rules below are constraints; the existing arrangement of screens is not.

## 3. Core concepts and product rules

| Concept | Meaning to the user |
| --- | --- |
| Saved item | A lasting record of a web link or uploaded PDF, with its title, source and personal metadata. |
| Capture | A dated local snapshot of a web page. An item can have multiple captures. |
| Saved page | The preserved page, presented with its original layout where possible. |
| Reading version | A cleaned presentation of suitable article content. |
| Reading PDF | A document prepared for reading, particularly on Kindle Scribe. An uploaded PDF keeps its original file. |
| Delivery | An explicit attempt to email a document to the configured Kindle address. |
| Tags and notes | Optional organization controlled by the owner. AI can suggest tags. |
| Browser folders | Information about where a bookmark came from, separate from owner-edited Attic tags and notes. |

Two actions must remain distinct and readily available:

| Action | Saves in Attic | Archives the page | Sends to Kindle |
| --- | --- | --- | --- |
| **Bookmark** | Yes | Yes, in the background | No |
| **Send to Kindle** | Yes | Yes, in the background | Yes, after a document is ready |

Additional fixed rules:

- Saving takes effect before background processing finishes. Capture, classification, search indexing, PDF preparation and delivery can have different outcomes.
- A failure in any background step must not make the saved item disappear. A usable document can still be delivered if page capture fails.
- Repeated ingestion should merge the same link without losing personal annotations or silently sending more emails.
- Successful captures remain available when a later capture fails. Capture history is retained until explicit removal.
- Deleting a browser bookmark does **not** delete anything from Attic. Only explicit removal in Attic does that.
- Removing something in Attic does not modify browser bookmarks, and routine browser synchronization must not immediately bring the removed item back.
- AI assistance is optional to successful saving and keyword search. It must not overwrite the owner's choices.

## 4. Features to represent

### Save and collect

- Paste a URL and choose Bookmark or Send to Kindle.
- Upload a PDF to keep it, with the option to send it to Kindle.
- Save the current page or a link through the Chromium extension.
- Optionally connect browser bookmarks: ingest existing bookmarks automatically, then follow additions, title changes and folder moves.
- Import a browser HTML bookmark file for other browsers or collections.
- Accept shared links from mobile devices.

Saving should need little interaction. Show clear acknowledgment that an item was saved or queued without implying that preservation or delivery has already completed. Re-saving an existing item should make its existing record easy to reach.

### Preserve and revisit

- Preserve static web snapshots with images, styles, tables and code where available.
- Open a saved page independently of whether the original site still exists.
- Offer a cleaned reading version for suitable content.
- Show capture dates, available versions and whether a capture is partial or blocked.
- Identify the original source and any recovered archive source.
- Request a fresh capture while retaining previous versions.
- Preserve uploaded PDF files unchanged.

Attic tries alternate sources when normal retrieval is blocked. The experience should explain the usable result and any limitation, without requiring the user to understand the retrieval pipeline. Saved pages are static; interactive site functionality is not preserved.

### Search and organize

- Search the whole collection, including titles, URLs, metadata, tags, notes and extracted page or PDF text.
- Display ranked results with matching snippets.
- Filter by domain, tags, saved date and capture status.
- Edit an item's title, notes and tags.
- See AI classification and suggested tags; accept or adjust suggestions without mandatory classification work.
- Retain browser folder information as useful context.

Keyword search is implemented. Semantic or conversational search is future scope. Image-only PDFs remain saved, but their contents are not searchable through OCR; the design must make that limitation understandable.

Design for thousands of saved items, including 10,000 or more. Searching, filtering and browsing should not assume a small set of recently loaded cards.

### Read and send to Kindle

- Read PDFs inside the app, including the iOS installed PWA.
- Navigate pages, see page position, zoom and download the PDF.
- Share the PDF through the device's share mechanism where supported.
- See author, source and publication information when available.
- Send a saved item to Kindle, reusing an available document where possible.
- See preparation and delivery status, and recover from failures.

Existing generated PDFs use a restrained LaTeX aesthetic, thicker text for e-ink and geometry fitted to Kindle Scribe. Preserve this reading quality. The interface should comfortably handle long technical documents with code, tables and images. A complete redesign of the PDF typesetting is not required by this brief.

Use **Send to Kindle** consistently for direct configured delivery. If manual Amazon upload or device sharing is offered as a fallback, label it separately. The current reader has some manual-send guidance that should be made clearer in the redesign.

### Own and maintain the archive

- See collection size and storage usage.
- Export a portable archive containing saved records and preserved files.
- Restore an exported archive, merging safely with existing records.
- Review import/restore progress and outcomes.
- Explicitly remove an item and its stored content.

Restore should not unexpectedly revisit websites or email documents. There is no automatic pruning in the current scope.

## 5. Platforms and entry points

### Responsive web app and installed PWA

The web app is the main library, search, management and reading experience. It must work on desktop and mobile, both in a browser and installed as a PWA. Access uses the owner's access key.

The PWA is not currently a fully offline archive browser. Preserved content no longer depends on the original website, but accessing it still requires reaching the Attic server. Avoid promising general offline availability.

### Chromium extension

The extension provides:

- A compact popup with Bookmark and Send to Kindle for the current page.
- Equivalent context-menu actions for links.
- Connection setup using the server address and access key.
- An option to **Automatically save browser bookmarks**, with an explicit browser permission step.
- Existing-bookmark ingestion, ongoing synchronization, a manual check and sync status.
- Durable queued saves and retry when the server becomes reachable again.
- A way to open the main library.

Cover first use, permission denied, disconnected, invalid credentials, server unavailable, queued save, accepted save and bulk-sync progress. Extension acknowledgment must not imply confirmed Kindle arrival.

### Android and iOS sharing

Mobile should use the PWA and existing system sharing tools rather than new native apps.

- **Android:** installed PWA share-target integration on supported browsers and secure deployments.
- **iOS:** Safari sharing through an Apple Shortcut connected to Attic, plus the installed PWA for the library and reader.

Design a concise “Save from anywhere” setup experience with platform-specific instructions and clear access to both save intentions. Do not imply that iOS offers the same native share-target integration as Android or that Attic directly reads Safari bookmarks.

## 6. Suggested screen coverage

These are responsibilities to cover, not a prescribed navigation structure.

| Surface | What the design needs to support |
| --- | --- |
| Access and first use | Connect with an access key, explain the product briefly, provide a useful empty-library starting point. |
| Library and search | Save actions, collection browsing, search, filters, snippets, status, pagination or an equivalent scalable pattern. |
| Item details | Source and metadata, notes/tags, classification suggestions, available reading formats, capture history, Kindle action and explicit removal. |
| Saved-page reader | Reading version versus original layout, capture identity/date, original source and partial-capture context. |
| PDF reader | Stable document rendering, navigation, zoom, metadata, download/share and Kindle action. |
| Import and archive management | PDF upload, HTML import, export/restore, progress, summaries and storage usage. |
| Save-from-anywhere setup | Extension installation/connection, bookmark permissions, Android PWA sharing and iOS Shortcut guidance. |
| Extension popup and options | Fast save actions, connection settings, automatic bookmark ingestion and meaningful sync feedback. |
| Connection and service guidance | Explain unavailable services or missing delivery setup and how to recover. |

SMTP delivery and AI/subscription connections currently rely on server-side setup. A status/help surface fits the design scope; a full in-app credentials and service-configuration dashboard would require additional implementation. Label any such proposed dashboard as an extension to current capabilities.

## 7. Essential user journeys

1. **Save now, organize later:** paste a link, choose Bookmark, receive acknowledgment, leave without filling in tags or notes. Return later to its preserved copy.
2. **Read on Kindle:** share a link or use the extension, choose Send to Kindle, see that it is saved, and follow document preparation and delivery independently.
3. **Continue normal browser bookmarking:** enable bookmark access once, understand the initial import, then use browser bookmarks normally. Offline saves catch up without repeated manual work.
4. **Find a half-remembered idea:** search for a phrase from the body of an old article, inspect snippets, refine by source or date, and open the appropriate saved version.
5. **Keep a PDF:** upload a document, open the original, find it later by extracted text or metadata, and optionally send it to Kindle. Explain when the file has no extractable text.
6. **Recover a disappeared page:** open a retained snapshot after the original becomes unavailable. A failed recapture leaves the earlier usable version easy to find.
7. **Add personal context:** edit notes and tags, optionally use AI suggestions, and preserve those choices through future browser updates.
8. **Recover from a problem:** understand which output failed and retry that operation without resaving the item or repeating a successful delivery.
9. **Move or back up the collection:** import bookmarks or restore an archive, see progress and a useful outcome summary, and avoid duplicate records or unexpected sending.
10. **Remove deliberately:** explicitly remove an item in Attic with clear consequences. Understand that browser bookmarks are unaffected and routine sync will not resurrect it.

## 8. States and feedback

Prefer concise, contextual feedback. Keep operational detail available without turning the library into a processing dashboard.

| Situation | What the user should understand or be able to do |
| --- | --- |
| Saved; background work pending | The item is already kept. Available actions do not need to wait for every step. |
| Capture complete | A preserved copy is available; show its date. |
| Capture partial | A copy exists, but some resources are missing; let the user open it. |
| Capture blocked or failed | The bookmark remains; show other usable outputs and a capture retry. |
| Classification unavailable | The item is usable; manual organization remains available. |
| Search indexing pending | A newly saved item may not yet appear in text search. |
| Search service unavailable | Explain the temporary search limitation without implying that the collection was lost. |
| PDF preparing or failed | Distinguish document status from the saved page; offer relevant recovery. |
| Kindle setup missing | The item is saved; delivery needs configuration. |
| Delivery queued or retrying | A delivery is pending. Avoid encouraging duplicate requests. |
| Email relay accepted | The sending service accepted the email; this does not confirm arrival on the Kindle. |
| Delivery failed | Show an actionable reason and appropriate retry. |
| Delivery outcome uncertain | Arrival is unknown; a manual resend may duplicate the document. Do not present this as a definite failure. |
| Extension offline | The save is queued locally and will retry. |
| Import partially successful | Show imported/merged/skipped or failed outcomes and what can be retried. |
| No results | Preserve the query and filters; make adjusting them easy. |

Readers must remain stable during background updates: no repeated loading flashes, jumping pages or loss of reading position. Destructive actions need clear confirmation; routine saving should not.

## 9. Scope boundaries

The features above describe current capabilities and the intended redesign of their experience. Richer in-app service setup is a proposal, as noted above.

The following are not required for this design release:

- Native Android or iOS apps, direct Safari bookmark ingestion or full offline PWA browsing.
- Multi-user accounts, collaboration, public sharing or social features.
- Semantic/vector search, conversational answers or OCR for scanned PDFs.
- Interactive website replay, recursive site crawling, video archiving or logged-in session capture.
- Scheduled recapture or automatic deletion/pruning.
- Read/unread workflows, favorites, highlights, PDF annotations or user-authored folder collections.

The designer may suggest future improvements, but should distinguish them from the core deliverable.

## 10. Requested design deliverables

- Information architecture and key user flows, including the relationship between saving, preservation, reading and delivery.
- Responsive high-fidelity designs for the web/PWA surfaces, extension popup/options and mobile sharing setup.
- A coherent component system: typography, color, spacing, controls, search results, status indicators, dialogs and feedback.
- Empty, loading, partial-success, failure, unavailable-service and large-collection states.
- Clickable prototypes for quick saving, Kindle delivery, search-to-reading, browser-bookmark onboarding and error recovery.
- Handoff notes explaining interaction behavior, action priorities, responsive changes and state transitions.

Use accessible contrast, visible keyboard focus, suitable touch targets and status labels that do not rely only on color. Test layouts with long titles, missing author/thumbnail data, many tags, nested browser-folder paths, multilingual text and lengthy PDFs. Content must remain recognizable without requiring every item to have an image.

A successful design lets the owner save with almost no thought, trust what has been preserved, find useful material later, and send it to Kindle without confusion about what each action has done.
