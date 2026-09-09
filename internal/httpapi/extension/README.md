# Save to Attic for Chromium

1. Download `/attic-chromium.zip` from your Attic server and extract it, or use this directory.
2. Open `chrome://extensions`, enable **Developer mode**, select **Load unpacked**, and choose the directory containing `manifest.json`.
3. Open the extension's **Options**, enter your Attic server address and access key, and approve access to that server.
4. Pin **Save to Attic** in Chromium's Extensions menu.

The toolbar popup exposes **Bookmark** and **Send to Kindle**. Both preserve the page; only Send to Kindle requests delivery. The same actions are available when right-clicking a page or link. Saves are retained locally until Attic acknowledges them, and retry after reconnection using the original request identity so delivery is not duplicated.

Saving the current page captures its loaded HTML and resources as MHTML in a temporary, inactive extension tab, then uploads that copy. This avoids revisiting the publisher from the server. The tab closes automatically, and the captured bytes remain in IndexedDB until uploaded. Kindle delivery starts after upload. If browser capture is unavailable, too large (32 MB limit), or rejected as a challenge page, Attic falls back to capturing the URL. Saving a link to a different page and automatic bookmark ingestion use server capture.

Enable **Automatically save browser bookmarks** in Options to grant optional bookmark access. Existing bookmarks are imported automatically; additions, title edits and folder moves are reconciled. Folder paths and original bookmark dates are preserved. Bookmark ingestion never requests Kindle delivery. Deleting a browser bookmark does not delete its Attic copy; removal in Attic remains explicit. Attic does not edit your browser bookmarks.

Reconciliation runs on bookmark changes, browser startup and once per minute while Chromium is running. Successful unchanged bookmarks are not resent. Failed batches remain in local storage across browser restarts. **Check now** retries immediately; the status in Options shows failures and the last successful reconciliation. Turning off automatic bookmarking clears its pending imports and revokes bookmark access while preserving Attic copies. Disconnect clears pending saves and server credentials.

The extension connects to an Attic server at its origin root (for example `https://attic.example.com`). Host access is requested when connecting; Chromium grants apply to a host regardless of port, while the configured address determines the actual destination. Use HTTPS on remote servers. The access key stays in local extension storage, not browser sync. The extension sends the explicitly saved page content and resources, link URLs, titles and opted-in bookmark metadata; it does not collect publisher cookies, browsing history or ChatGPT credentials.

This is an unpacked extension. Keep its directory in place and click **Reload** at `chrome://extensions` after updating the files. Browser bookmark access does not provide access to mobile Safari bookmarks.

Run `node tests/extension.test.mjs` from the repository root for behavior checks.

## Search from the address bar

Type `a`, press Tab, then type a query. Suggestions are matching saved items from
your configured Attic server; choose one to open its reader, or press Enter to
search the full library. Search needs Attic’s search index to be configured.
Only queries entered in this keyword mode are sent to Attic, with your existing
access key. Normal address-bar typing is not sent.

Download updates from Attic Settings, replace the unpacked files in the existing
extension directory, then click Reload at `chrome://extensions`.
