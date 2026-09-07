# Save to Attic for Chromium

1. Download `/attic-chromium.zip` from your Attic server and extract it. Alternatively, use this source directory.
2. Open `chrome://extensions`, enable **Developer mode**, click **Load unpacked**, and select the extracted `attic` directory containing `manifest.json`.
3. Open the extension's **Options**. Enter your Attic server address and access key (`BEARER_TOKEN` in the server configuration), then click **Connect to Attic** and approve access to that server.
4. Pin **Save to Attic** in Chromium's Extensions menu.

Click the toolbar button to save the current article, or right-click a link and choose **Save link to Attic**. A ✓ badge means the server accepted the URL for processing; PDF preparation continues on the server. A ! badge indicates a submission problem; hover over the button for details. Open your library from Options to check progress and download PDFs.

The extension works with any reachable Attic server origin (for example, `https://attic.example.com`). Reverse proxies must expose Attic at the origin root. Use HTTPS for a remote server: HTTP sends the access key without encryption. Host access is requested only when connecting; Chromium host grants apply to the host regardless of port. The configured address determines where requests go. No publisher-page content, cookies, browser history, or ChatGPT credentials are collected. Only the article URL is submitted. The access key remains in local extension storage, not browser sync. **Disconnect** removes it and revokes server access.

This is an unpacked extension, not a Chrome Web Store release. Keep the extracted directory in place. After updating its files, click **Reload** at `chrome://extensions`. Changing deployment only requires reconnecting to the new server in Options.

Run the behavior checks from `implementation/attic` with `node --test tests/extension.test.mjs`.
