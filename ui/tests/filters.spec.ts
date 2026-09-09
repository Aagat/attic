import { test, expect } from "@playwright/test";

test("search consumes extension links, stays out of the URL, and survives reader navigation", async ({
  page,
}) => {
  await page.goto("/?q=web");
  await expect(
    page.getByRole("textbox", { name: "Search library" }),
  ).toHaveValue("web");
  await expect(page).toHaveURL("/");
  const search = page.getByRole("textbox", { name: "Search library" });
  await search.fill("small");
  await expect(page).toHaveURL("/");
  await expect(page.getByRole("article")).toHaveCount(1);
  await page.getByRole("article").getByRole("link").click();
  await page.goBack();
  await expect(search).toHaveValue("small");
  await expect(page.getByRole("article")).toHaveCount(1);
  await page.getByRole("button", { name: "Clear search", exact: true }).click();
  await expect(page.getByRole("article")).toHaveCount(8);
});

test("source and tag filters autocomplete from the entire library", async ({
  page,
}) => {
  await page.goto("/?page=2");
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  const source = page.getByRole("combobox", { name: "Source", exact: true });
  await source.fill("works");
  await expect(
    page.getByRole("option", { name: "worksinprogress.co", exact: true }),
  ).toBeVisible();
  await source.press("ArrowDown");
  await source.press("Enter");
  await expect(source).toHaveValue("worksinprogress.co");
  await source.fill("");
  const tags = page.getByRole("combobox", { name: "Tags", exact: true });
  await tags.fill("architecture");
  await expect(
    page.getByRole("option", { name: "architecture", exact: true }),
  ).toBeVisible();
  await page.getByRole("option", { name: "architecture", exact: true }).click();
  await expect(tags).toHaveValue("architecture");
  await expect(page.getByRole("listbox")).toHaveCount(0);
});
