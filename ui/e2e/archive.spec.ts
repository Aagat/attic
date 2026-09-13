import { test, expect } from "@playwright/test";
import { samplePdf } from "./pdf";
// Keep server/session assertions independent of service-worker navigation races.
// Offline shell behavior is covered separately by the preview PWA tests.
test.use({ serviceWorkers: "block" });
const key = "attic-e2e-owner-key";
test("real archive: session, upload, edit, search, delivery, backup, restore and logout", async ({
  page,
  context,
  request,
}, info) => {
  test.setTimeout(180000);
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const title = `Reading ${info.project.name} ${Date.now()}`;
  await page.goto("/settings");
  await expect(
    page.getByRole("heading", { name: "Connect to Attic" }),
  ).toBeVisible();
  await page.getByLabel("Access key").fill("wrong");
  await page.getByRole("button", { name: "Connect to Attic" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await page.getByLabel("Access key").fill(key);
  await page.getByRole("button", { name: "Connect to Attic" }).click();
  await expect(
    page.getByRole("heading", { name: "Settings", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Choose PDF", exact: true }).click();
  await page.getByLabel("PDF file", { exact: true }).setInputFiles({
    name: title + ".pdf",
    mimeType: "application/pdf",
    buffer: samplePdf(title),
  });
  await page
    .getByRole("button", { name: "Keep original PDF", exact: true })
    .click();
  await expect(page).toHaveURL(/\/items\//);
  const itemURL = page.url();
  await expect(page.locator("canvas")).toBeVisible();
  await expect
    .poll(() =>
      page.locator("canvas").evaluate((e: HTMLCanvasElement) => e.width),
    )
    .toBeGreaterThan(0);
  await page.reload();
  await expect(page.locator("canvas")).toBeVisible();
  await page.getByRole("button", { name: "Hide item information" }).click();
  await expect(
    page.getByRole("complementary", { name: "Item information" }),
  ).toBeHidden();
  await page.getByRole("button", { name: "Item details", exact: true }).click();
  await page.getByLabel("Title", { exact: true }).fill(title);
  await page
    .getByRole("textbox", { name: "Notes", exact: true })
    .fill("Unique searchable note " + title);
  await page.getByLabel("Tags (comma separated)").fill("e2e, reading");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download", exact: true }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toContain(".pdf");
  const emailsBefore = (
    await (await request.get("http://mailpit:8025/api/v1/messages")).json()
  ).total;
  await page
    .getByRole("button", { name: "Send to Kindle", exact: true })
    .click();
  await expect
    .poll(async () => {
      const r = await request.get("http://mailpit:8025/api/v1/messages");
      return (await r.json()).total;
    })
    .toBeGreaterThan(emailsBefore);
  await page.goto("/?q=" + encodeURIComponent(title));
  await expect(
    page.getByRole("link", { name: title, exact: true }),
  ).toBeVisible({ timeout: 120000 });
  await page.getByLabel("Item type").selectOption("PDF");
  await expect(
    page.getByRole("link", { name: title, exact: true }),
  ).toBeVisible({ timeout: 120000 });
  await page.goto("/settings");
  const backupPromise = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Export archive", exact: true })
    .click();
  const backup = await backupPromise;
  const backupPath = await backup.path();
  expect(backup.suggestedFilename()).toBe("attic.zip");
  await page.goto(itemURL);
  await page.getByRole("button", { name: "Item details", exact: true }).click();
  await page.getByRole("button", { name: "Remove item", exact: true }).click();
  await page
    .getByRole("dialog", { name: "Remove this item?" })
    .getByRole("button", { name: "Remove item", exact: true })
    .click();
  await expect(page).toHaveURL(/\/$/);
  // Removal is deferred during the undo window. Restore only after the real
  // server confirms deletion, otherwise the pending removal can delete it again.
  const itemID = new URL(itemURL).pathname.split("/").pop();
  await expect
    .poll(async () =>
      (await page.request.get(`/api/v1/items/${itemID}`)).status(),
    )
    .toBe(404);
  await page.goto("/settings");
  await page
    .getByLabel("Restore archive", { exact: true })
    .setInputFiles(backupPath!);
  await expect(page.getByText(/records restored/)).toBeVisible();
  await page.goto(itemURL);
  await expect(page.locator("canvas")).toBeVisible();
  await page.goto("/settings");
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(
    page.getByRole("heading", { name: "Connect to Attic" }),
  ).toBeVisible();
  await page.goto(itemURL);
  await expect(
    page.getByRole("heading", { name: "Connect to Attic" }),
  ).toBeVisible();
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain(
    key,
  );
  expect(
    (await context.cookies()).some((c) => c.name === "attic_session"),
  ).toBeFalsy();
  expect(errors).toEqual([]);
});
test("shared bookmark persists; responsive pages contain no simulated controls", async ({
  page,
  request,
}, info) => {
  const unique = Date.now();
  const url = `https://example.com/?e2e=${info.project.name}-${unique}`;
  await page.goto("/share?url=" + encodeURIComponent(url));
  await page.getByLabel("Access key").fill(key);
  await page.getByRole("button", { name: "Connect to Attic" }).click();
  await expect(page.getByLabel("URL to save")).toHaveValue(url);
  await page.getByRole("button", { name: /^Bookmark/ }).click();
  await expect(page.getByLabel("URL to save")).toHaveValue("");
  await page.goto("/");
  await expect(
    page.getByRole("link", { name: /example.com/ }).first(),
  ).toBeVisible();
  for (const route of ["/", "/settings", "/setup"]) {
    await page.goto(route);
    await expect(page.getByRole("main")).toBeVisible();
    await page.screenshot({
      path: info.outputPath(route.replaceAll("/", "_") + ".png"),
      fullPage: true,
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await expect(page.getByText("UI PREVIEW", { exact: true })).toHaveCount(0);
    await expect(page.getByText("Preview state controls")).toHaveCount(0);
  }
  const manifest = await (await request.get("/manifest.webmanifest")).json();
  expect(manifest.share_target.action).toBe("/share");
});

test("HTML imports merge and server pagination reaches all matching records", async ({
  page,
}, info) => {
  test.setTimeout(180000);
  const prefix = `Import ${info.project.name} ${Date.now()}`;
  const batchTag = `batch-${info.project.name}-${Date.now()}`;
  await page.goto("/settings");
  await page.getByLabel("Access key").fill(key);
  await page.getByRole("button", { name: "Connect to Attic" }).click();
  const html = Array.from(
    { length: 18 },
    (_, i) =>
      `<DT><A HREF="https://example.invalid/${encodeURIComponent(prefix)}/${i}" TAGS="${batchTag}">${prefix} record ${i}</A>`,
  ).join("");
  const file = {
    name: "bookmarks.html",
    mimeType: "text/html",
    buffer: Buffer.from(
      "<!DOCTYPE NETSCAPE-Bookmark-file-1><DL>" + html + "</DL>",
    ),
  };
  await page.getByLabel("Import HTML bookmarks").setInputFiles(file);
  await expect(page.getByText(/18 imported/)).toBeVisible();
  await page.getByLabel("Import HTML bookmarks").setInputFiles(file);
  await expect(page.getByText(/18 merged/)).toBeVisible();
  // Full-text search tolerates typos and drops unmatched terms. Scope this
  // pagination fixture with an exact tag so earlier batches cannot match it.
  await page.goto("/?q=" + encodeURIComponent(prefix) + "&tag=" + batchTag);
  await expect(page.getByText("18 results", { exact: true })).toBeVisible({
    timeout: 150000,
  });
  await expect(
    page.getByRole("region", { name: "Saved items" }).locator("article"),
  ).toHaveCount(8);
  await page.getByRole("button", { name: "Next page", exact: true }).click();
  await expect(page).toHaveURL(/page=2/);
  await expect(
    page.getByRole("region", { name: "Saved items" }).locator("article"),
  ).toHaveCount(8);
  await page.getByRole("button", { name: "Next page", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Saved items" }).locator("article"),
  ).toHaveCount(2);
  await page.reload();
  await expect(
    page.getByRole("region", { name: "Saved items" }).locator("article"),
  ).toHaveCount(2);
});
