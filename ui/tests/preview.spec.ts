import { test, expect } from "@playwright/test";
test("library search, filters, no results and pagination", async ({ page }) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Library", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Next page", exact: true }).click();
  await expect(page.getByText("Showing 9–12 of 12")).toBeVisible();
  await page
    .getByRole("textbox", { name: "Search library" })
    .fill("distributed");
  await expect(page.getByRole("article")).toHaveCount(4);
  await page
    .getByRole("textbox", { name: "Search library" })
    .fill("a word not present");
  await expect(
    page.getByRole("heading", { name: "Nothing here matches yet" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Clear search and filters" }).click();
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await page
    .getByLabel("Capture status", { exact: true })
    .selectOption("Capture failed");
  await page.getByRole("button", { name: "Show 1 items" }).click();
  await expect(page.getByRole("article")).toHaveCount(1);
  await expect(
    page.getByRole("link", { name: "A small web is still possible" }),
  ).toBeVisible();
});
test("bookmark, edit, reload, deduplicate and remove", async ({ page }) => {
  await page.goto("/");
  await page.locator("header").getByRole("button", { name: /Save/ }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("URL to save").fill("https://example.com/kept");
  await dialog.getByRole("button", { name: /^Bookmark/ }).click();
  await page
    .getByRole("link", { name: "example.com / kept", exact: true })
    .click();
  await page.getByRole("button", { name: "Item details", exact: true }).click();
  await dialog
    .getByLabel("Title", { exact: true })
    .fill("My durable reference");
  await dialog.getByLabel("Notes", { exact: true }).fill("needle-for-search");
  await dialog.getByLabel("Tags (comma separated)").fill("reference, personal");
  await dialog.getByRole("button", { name: "Save changes" }).click();
  await expect(dialog).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "My durable reference",
  );
  await page.getByRole("link", { name: "Attic library", exact: true }).click();
  await page.locator("header").getByRole("button", { name: /Save/ }).click();
  await dialog.getByLabel("URL to save").fill("https://example.com/kept");
  await dialog.getByRole("button", { name: /^Send to Kindle/ }).click();
  await expect(dialog).toHaveCount(0);
  await page
    .getByRole("link", { name: "My durable reference", exact: true })
    .click();
  await page.getByRole("button", { name: "Item details", exact: true }).click();
  await dialog
    .getByRole("button", { name: "Remove item", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "Remove this item?" })
    .getByRole("button", { name: "Remove permanently" })
    .click();
  await expect(page).toHaveURL("/");
  await expect(
    page.getByRole("link", { name: "My durable reference", exact: true }),
  ).toHaveCount(0);
});
test("recapture retains earlier copies and sent email can be resent", async ({
  page,
}) => {
  await page.goto("/items/sample-1");
  await page.getByRole("button", { name: "Request fresh capture" }).click();
  await expect(
    page.getByRole("button", { name: "Capture queued" }),
  ).toBeDisabled();
  await expect(page.getByRole("button", { name: /18 Aug 2026/ })).toBeVisible();
  await page.goto("/items/sample-3");
  await expect(page.getByText("Email Sent", { exact: true })).toBeVisible();
  await expect(page.getByText(/The email relay accepted/)).toHaveCount(0);
  await page.getByRole("button", { name: "Resend to Kindle" }).click();
  await expect(
    page.getByText("Preparing document", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("dialog", { name: "Send this document again?" }),
  ).toHaveCount(0);
});
test("settings import merges duplicates; export is a real download", async ({
  page,
}) => {
  await page.goto("/settings");
  await page.getByLabel("Import HTML bookmarks").setInputFiles({
    name: "bookmarks.html",
    mimeType: "text/html",
    buffer: Buffer.from(
      '<DL><DT><A HREF="https://example.com/one">One</A><DT><A HREF="https://example.com/one">Duplicate</A><DT><A HREF="javascript:alert(1)">Unsafe</A></DL>',
    ),
  });
  await expect(
    page.getByText("1 added · 1 merged.", { exact: false }),
  ).toBeVisible();
  const downloading = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export archive" }).click();
  expect((await downloading).suggestedFilename()).toBe("attic-ui-preview.json");
});
test("responsive screens and keyboard dialogs have no page overflow or runtime errors", async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  for (const path of [
    "/",
    "/items/sample-1",
    "/items/sample-5",
    "/settings",
    "/setup",
    "/connect",
  ]) {
    await page.goto(path);
    await expect(page.locator("h1")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: `test-results/${info.project.name}-${path.replaceAll("/", "-") || "library"}.png`,
      fullPage: true,
    });
  }
  await page.goto("/");
  await page.locator("header").getByRole("button", { name: /Save/ }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(errors).toEqual([]);
});
test("production PWA manifest and precached shell", async ({ page }) => {
  await page.goto("/");
  const manifest = await (
    await page.request.get("/manifest.webmanifest")
  ).json();
  expect(manifest.display).toBe("standalone");
  expect(manifest.share_target.action).toBe("/share");
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  expect(
    await page.evaluate(async () => {
      for (const key of await caches.keys()) {
        const cache = await caches.open(key);
        const requests = await cache.keys();
        if (requests.some((r) => new URL(r.url).pathname === "/index.html"))
          return true;
      }
      return false;
    }),
  ).toBe(true);
});
test("offline reload uses the installed shell", async ({
  page,
  context,
  browserName,
}) => {
  test.skip(
    browserName === "webkit",
    "WebKit automation reports an internal error on offline reload. Validate installed Safari launch on a real device.",
  );
  await page.goto("/");
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  await page.reload();
  await context.setOffline(true);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Library", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Offline — reconnect to access your archive"),
  ).toBeVisible();
  await context.setOffline(false);
});
function samplePdf() {
  const stream = "BT /F1 20 Tf 40 200 Td (Attic PDF test) Tj ET";
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((obj, i) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${i + 1} 0 obj\n${obj}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 6\n0000000000 65535 f \n${offsets
    .slice(1)
    .map((n) => `${String(n).padStart(10, "0")} 00000 n \n`)
    .join("")}trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`;
  return Buffer.from(pdf);
}
test("uploaded PDF renders, survives reload and downloads the original", async ({
  page,
}) => {
  await page.goto("/settings");
  await page.getByRole("button", { name: "Choose PDF", exact: true }).click();
  await page.getByLabel("PDF file", { exact: true }).setInputFiles({
    name: "attic-test.pdf",
    mimeType: "application/pdf",
    buffer: samplePdf(),
  });
  await page.getByRole("button", { name: "Keep original PDF" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "attic-test",
  );
  await expect
    .poll(() =>
      page.locator("canvas").evaluate((c: HTMLCanvasElement) => c.width),
    )
    .toBe(420);
  await page.reload();
  await expect
    .poll(() =>
      page.locator("canvas").evaluate((c: HTMLCanvasElement) => c.width),
    )
    .toBe(420);
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download", exact: true }).click();
  expect((await download).suggestedFilename()).toBe("attic-test.pdf");
  await expect(
    page.getByRole("button", { name: "Next PDF page" }),
  ).toBeDisabled();
});
test("ten thousand records keep pagination compact and search the collection", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("article")).toHaveCount(8);
  await page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const req = indexedDB.open("attic-preview-records", 1);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    const base = await new Promise<any>((resolve) => {
      const req = db.transaction("records").objectStore("records").get("items");
      req.onsuccess = () => resolve(req.result[0]);
    });
    const many = Array.from({ length: 10000 }, (_, index) => ({
      ...base,
      id: `large-${index}`,
      title: `Archive record ${index}`,
      notes: index === 9999 ? "needle-at-the-end" : "",
    }));
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction("records", "readwrite");
      tx.objectStore("records").put(many, "items");
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
    db.close();
  });
  await page.reload();
  await expect(page.getByRole("article")).toHaveCount(8);
  await expect(
    page.getByRole("navigation", { name: "Pagination" }).getByRole("button"),
  ).toHaveCount(5);
  await page
    .getByRole("textbox", { name: "Search library" })
    .fill("needle-at-the-end");
  await expect(page.getByRole("article")).toHaveCount(1);
  await expect(
    page.getByRole("link", { name: "Archive record 9999" }),
  ).toBeVisible();
});

test("reader has one toolbar and a collapsible information panel", async ({
  page,
}) => {
  await page.goto("/items/sample-1");
  await expect(page.locator("header")).toHaveCount(1);
  await expect(
    page
      .locator("header")
      .getByRole("button", { name: "Item details", exact: true }),
  ).toBeVisible();
  await expect(
    page
      .locator("header")
      .getByRole("button", { name: /Save link|Upload PDF/ }),
  ).toHaveCount(0);
  await expect(
    page.locator("header").getByRole("link", { name: "Library", exact: true }),
  ).toHaveCount(0);
  const content = page
    .getByRole("tabpanel", { name: "Reading version" })
    .locator("..");
  const before = await content.boundingBox();
  await page.getByRole("button", { name: "Hide item information" }).click();
  await expect(
    page.getByRole("complementary", { name: "Item information" }),
  ).toBeHidden();
  if (page.viewportSize()!.width >= 1024)
    expect((await content.boundingBox())!.width).toBeGreaterThan(before!.width);
  await page.getByRole("button", { name: "Show item information" }).click();
  await expect(
    page.getByRole("complementary", { name: "Item information" }),
  ).toBeVisible();
  await page.goto("/items/sample-5");
  await page.getByLabel("PDF page", { exact: true }).fill("4");
  await page.getByRole("button", { name: "Hide item information" }).click();
  await expect(page.getByLabel("PDF page", { exact: true })).toHaveValue("4");
  await page.goto("/");
  await expect(
    page.locator("header").getByRole("link", { name: "Settings", exact: true }),
  ).toHaveCount(0);
  await page.goto("/settings");
  await expect(
    page.getByRole("link", { name: "Open library", exact: true }),
  ).toHaveCount(0);
});

test("save and Kindle actions work without secure-context randomUUID", async ({
  page,
}) => {
  await page.addInitScript(() =>
    Object.defineProperty(crypto, "randomUUID", { value: undefined }),
  );
  await page.goto("/items/sample-1");
  await page
    .getByRole("button", { name: "Send to Kindle", exact: true })
    .click();
  await expect(
    page.getByText("Kindle preparation requested.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("crypto.randomUUID is not a function"),
  ).toHaveCount(0);
  await page.getByRole("link", { name: "Attic library", exact: true }).click();
  await page.locator("header").getByRole("button", { name: /Save/ }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("URL to save").fill("https://example.com/http-save");
  await dialog.getByRole("button", { name: /^Bookmark/ }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "example.com / http-save", exact: true }),
  ).toBeVisible();
});

test("article PDF tab appears only with a document and renders after switching views", async ({
  page,
}) => {
  await page.goto("/items/sample-1");
  await expect(page.getByRole("tab", { name: "PDF", exact: true })).toHaveCount(
    0,
  );
  await page.goto("/settings");
  await page.getByRole("button", { name: "Choose PDF", exact: true }).click();
  await page.getByLabel("PDF file", { exact: true }).setInputFiles({
    name: "article-reading.pdf",
    mimeType: "application/pdf",
    buffer: samplePdf(),
  });
  await page.getByRole("button", { name: "Keep original PDF" }).click();
  await expect(page.locator("canvas")).toBeVisible();
  // Model a saved article with an available generated document using the real stored PDF.
  await page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve) => {
      const request = indexedDB.open("attic-preview-records", 1);
      request.onsuccess = () => resolve(request.result);
    });
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction("records", "readwrite");
      const store = tx.objectStore("records");
      const request = store.get("items");
      request.onsuccess = () => {
        const items = request.result;
        items[0].kind = "Article";
        items[0].url = "https://example.com/article";
        store.put(items, "items");
      };
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
    db.close();
  });
  await page.reload();
  await expect(
    page.getByRole("tab", { name: "Reading version", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(
    page
      .locator("header")
      .getByRole("button", { name: "Download PDF", exact: true }),
  ).toHaveCount(0);
  await expect(
    page
      .getByRole("complementary", { name: "Item information" })
      .getByRole("button", { name: "Download PDF", exact: true }),
  ).toBeEnabled();
  for (const view of ["Reading version", "Original layout"]) {
    await page.getByRole("tab", { name: "PDF", exact: true }).click();
    await expect
      .poll(() =>
        page.locator("canvas").evaluate((c: HTMLCanvasElement) => c.width),
      )
      .toBe(420);
    await expect(page.getByLabel("PDF page", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Zoom in", exact: true }).click();
    await page.getByRole("tab", { name: view, exact: true }).click();
    await expect(page.locator("canvas")).toHaveCount(0);
  }
});

test("sidebar generates PDF without requesting Kindle delivery", async ({
  page,
}) => {
  await page.goto("/items/sample-1");
  const sidebar = page.getByRole("complementary", { name: "Item information" });
  await expect(
    page
      .locator("header")
      .getByRole("button", { name: "Download PDF", exact: true }),
  ).toHaveCount(0);
  await sidebar
    .getByRole("button", { name: "Generate PDF", exact: true })
    .click();
  await expect(
    sidebar.getByRole("button", { name: "Generating PDF…", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByText("PDF generation requested. No email will be sent.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Send to Kindle", exact: true }),
  ).toBeEnabled();
  await expect(
    sidebar.getByText("Not requested", { exact: true }),
  ).toBeVisible();
});

test("browser recovery explains availability and fits the mobile reader", async ({
  page,
}) => {
  await page.goto("/items/sample-1");
  await page
    .getByRole("button", { name: "Continue capture", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Continue capture",
    exact: true,
  });
  await expect(dialog.getByRole("status")).toHaveText(
    "Browser recovery is available when connected to your Attic server.",
  );
  const bounds = await dialog.boundingBox();
  expect(bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  await dialog.getByRole("button", { name: "Close dialog" }).click();
  await expect(dialog).toHaveCount(0);
});
