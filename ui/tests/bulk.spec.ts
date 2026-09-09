import { test, expect } from "@playwright/test";

test("bulk tags and Kindle delivery apply to selected items", async ({
  page,
}) => {
  await page.goto("/");
  const cards = page.getByRole("article");
  await expect(cards).toHaveCount(8);
  const firstURL = await cards.nth(0).getByRole("link").getAttribute("href");
  await cards.nth(0).locator("p").first().click();
  await cards.nth(1).locator("p").first().click();
  await page.getByLabel("Tags to add").fill("bulk-test");
  await page.getByRole("button", { name: "Add tags", exact: true }).click();
  await expect(page.getByText("2 updated.", { exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "Search library" }).fill("bulk-test");
  await expect(cards).toHaveCount(2);
  if (!(await page.getByRole("button", { name: "Select this page" }).count())) {
    await page.getByRole("article").first().locator("p").first().click();
  }
  if (await page.getByRole("button", { name: "Select this page" }).isEnabled())
    await page.getByRole("button", { name: "Select this page" }).click();
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
  await cards.nth(0).locator("p").first().click();
  await cards.nth(1).locator("p").first().click();
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
  await cards.nth(0).locator("p").first().click();
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
  if (!(await page.getByRole("button", { name: "Select this page" }).count())) {
    await page.getByRole("article").first().locator("p").first().click();
  }
  if (await page.getByRole("button", { name: "Select this page" }).isEnabled())
    await page.getByRole("button", { name: "Select this page" }).click();
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
  if (!(await page.getByRole("button", { name: "Select this page" }).count())) {
    await page.getByRole("article").first().locator("p").first().click();
  }
  if (await page.getByRole("button", { name: "Select this page" }).isEnabled())
    await page.getByRole("button", { name: "Select this page" }).click();
  await page.getByRole("button", { name: "Retry captures" }).click();
  await expect(
    page.getByRole("button", { name: "Clear selection" }),
  ).toHaveCount(0);
  await page.goto(url!);
  await expect(
    page.getByRole("button", { name: "Capture queued" }),
  ).toBeDisabled();
});

test("rows select outside titles, sidebar owns actions, and keyboard and double-click open readers", async ({
  page,
}) => {
  await page.goto("/");
  const row = page.getByRole("article").first();
  const title = row.getByRole("link");
  const url = await title.getAttribute("href");
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await row.locator("p").first().click();
  await expect(row).toHaveAttribute("data-selected", "true");
  await expect(
    page
      .getByRole("complementary", { name: "Library actions" })
      .getByRole("button", { name: "Send to Kindle", exact: true }),
  ).toBeVisible();
  await row.locator("p").first().click();
  await expect(row).toHaveAttribute("data-selected", "false");
  await row.focus();
  await page.keyboard.press("Space");
  await expect(row).toHaveAttribute("data-selected", "true");
  await page.keyboard.press("Space");
  await title.click();
  await expect(page).toHaveURL(url!);
  await page.goBack();
  await expect(
    page.getByRole("button", { name: "Remove selected" }),
  ).toHaveCount(0);
  await page.getByRole("article").first().locator("p").first().dblclick();
  await expect(page).toHaveURL(url!);
});
