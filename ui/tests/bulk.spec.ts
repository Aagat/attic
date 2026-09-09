import { test, expect } from "@playwright/test";

test("bulk tags and Kindle delivery apply to selected items", async ({
  page,
}) => {
  await page.goto("/");
  const cards = page.getByRole("article");
  await expect(cards).toHaveCount(8);
  const firstURL = await cards.nth(0).getByRole("link").getAttribute("href");
  await cards.nth(0).getByRole("checkbox").check();
  await cards.nth(1).getByRole("checkbox").check();
  await page.getByLabel("Tags to add").fill("bulk-test");
  await page.getByRole("button", { name: "Add tags", exact: true }).click();
  await expect(page.getByText("2 updated.", { exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "Search library" }).fill("bulk-test");
  await expect(cards).toHaveCount(2);
  await page.getByRole("checkbox", { name: "Select this page" }).check();
  await page
    .getByRole("button", { name: "Send to Kindle", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Clear selection" }),
  ).toHaveCount(0);
  await page.goto(firstURL!);
  await expect(
    page.getByText("Preparing document", { exact: true }),
  ).toBeVisible();
});

test("bulk removal can be undone; confirmed removal persists after the grace period", async ({
  page,
}) => {
  await page.goto("/");
  const cards = page.getByRole("article");
  await expect(cards).toHaveCount(8);
  const titles = await cards.locator("a").allTextContents();
  await cards.nth(0).getByRole("checkbox").check();
  await cards.nth(1).getByRole("checkbox").check();
  await page.getByRole("button", { name: "Remove selected" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Remove items", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: titles[0], exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(
    page.getByRole("link", { name: titles[0], exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("link", { name: titles[1], exact: true }),
  ).toBeVisible();
  await cards.nth(0).getByRole("checkbox").check();
  await page.getByRole("button", { name: "Remove selected" }).click();
  await page.clock.install();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Remove items", exact: true })
    .click();
  await page.clock.fastForward(11000);
  await expect(
    page.getByRole("button", { name: "Undo", exact: true }),
  ).toHaveCount(0);
  await page.reload();
  await expect(
    page.getByRole("link", { name: titles[0], exact: true }),
  ).toHaveCount(0);
});

test("selection resets on navigation and retry only targets failed captures", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByRole("checkbox", { name: "Select this page" }).check();
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(
    page.getByRole("button", { name: "Remove selected" }),
  ).toHaveCount(0);
  await page.goto("/?status=Capture+failed");
  await expect(page.getByRole("article")).toHaveCount(1);
  const url = await page
    .getByRole("article")
    .getByRole("link")
    .getAttribute("href");
  await page.getByRole("checkbox", { name: "Select this page" }).check();
  await page.getByRole("button", { name: "Retry captures" }).click();
  await expect(
    page.getByRole("button", { name: "Clear selection" }),
  ).toHaveCount(0);
  await page.goto(url!);
  await expect(
    page.getByRole("button", { name: "Capture queued" }),
  ).toBeDisabled();
});
