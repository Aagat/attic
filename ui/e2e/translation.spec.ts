import { test, expect } from "@playwright/test";

test("reader shows preparation, translated text, and the preserved original", async ({
  page,
}) => {
  let item = {
    id: "saved",
    kind: "bookmark",
    title: "Artículo",
    url: "https://example.com/story",
    saved_at: "2026-09-09T12:00:00Z",
    capture_status: "complete",
    text_available: true,
    delivery_status: "not_requested",
    pdf_status: "not_requested",
    output_language: "",
    reading_available: false,
    job_id: "",
    captures: [
      {
        id: "capture",
        created_at: "2026-09-09T12:00:00Z",
        status: "complete",
        missing: [],
      },
    ],
  };
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/translate")) {
      item = {
        ...item,
        output_language: route.request().postDataJSON().language,
        pdf_status: "processing",
        job_id: "translation",
      };
    }
    if (path.endsWith("/reading"))
      return route.fulfill({
        contentType: "text/html; charset=utf-8",
        body: "<p>Translated article</p>",
      });
    if (path.includes("/captures/"))
      return route.fulfill({
        contentType: "text/html; charset=utf-8",
        body: "<p>Original español</p>",
      });
    return route.fulfill({ json: path === "/api/v1/session" ? {} : item });
  });
  await page.goto("/items/saved");
  await expect(
    page.getByRole("heading", { name: "Artículo", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Translate", exact: true }).click();
  await expect(page.getByText(/Preparing your translation/)).toBeVisible();
  item = { ...item, reading_available: true, pdf_status: "ready" };
  await expect(
    page.locator('iframe[title="Translated reading version"]'),
  ).toBeVisible({ timeout: 10000 });
  await expect(
    page
      .frameLocator('iframe[title="Translated reading version"]')
      .getByText("Translated article"),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Original layout" }).click();
  await expect(
    page
      .frameLocator('iframe[title="Saved original layout"]')
      .getByText("Original español"),
  ).toBeVisible();
  await page.getByRole("button", { name: "Original", exact: true }).click();
  await expect(
    page.locator('iframe[title="Saved reading version"]'),
  ).toBeVisible();
});
