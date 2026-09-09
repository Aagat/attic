// Build with pnpm build, serve with pnpm preview, then run node tests/recovery-ui.mjs.
// All backend calls are mocked; no saved items or browser profiles are changed.
import {
  chromium,
  devices,
  expect,
} from "../ui/node_modules/@playwright/test/index.mjs";
const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({
    ...devices["Pixel 7"],
    serviceWorkers: "block",
  });
  const page = await context.newPage();
  const pixel = await page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = 1024;
    canvas.height = 768;
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "white";
    ctx.fillRect(0, 0, 1024, 768);
    return canvas.toDataURL("image/png");
  });
  let state = {
    status: "idle",
    url: "https://example.com/article",
    title: "Article",
    message: "Ready",
    width: 1024,
    height: 768,
    screenshot: pixel,
  };
  const actions = [];
  let gets = 0;
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    let data = {};
    if (url.pathname.endsWith("/recovery")) {
      if (route.request().method() === "POST") {
        const action = route.request().postDataJSON();
        actions.push(action);
        if (action.action === "start")
          state = { ...state, status: "working", message: "Trying recovery" };
        if (action.action === "takeover")
          state = { ...state, status: "needs_input", message: "Your turn" };
        if (action.action === "capture")
          state = { ...state, status: "saved", message: "Saved browser copy" };
      } else gets++;
      data = state;
    } else if (url.pathname === "/api/v1/items/saved")
      data = {
        id: "saved",
        kind: "bookmark",
        url: state.url,
        title: "Blocked article",
        capture_status: "blocked",
        delivery_status: "not_requested",
        captures: [],
      };
    else if (url.pathname.endsWith("/items"))
      data = { items: [], total: 1, storage_bytes: 0 };
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(data),
    });
  });
  await page.goto(
    new URL(
      "/items/saved",
      process.env.ATTIC_UI_BASE_URL || "http://localhost:4173",
    ).href,
  );
  await page
    .getByRole("button", { name: "Continue capture", exact: true })
    .click();
  await page.getByRole("button", { name: "Take control", exact: true }).click();
  const image = page.getByAltText("Recovery browser page");
  await expect(image).toBeVisible();
  const box = await image.boundingBox();
  await image.click({ position: { x: box.width / 2, y: box.height / 2 } });
  await expect
    .poll(() =>
      actions.some(
        (a) =>
          a.action === "click" &&
          Math.abs(a.x - 512) <= 4 &&
          Math.abs(a.y - 384) <= 4,
      ),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Scroll down", exact: true }).click();
  await expect
    .poll(() => actions.some((a) => a.action === "scroll" && a.delta_y === 500))
    .toBe(true);
  await page.getByLabel("Hide sensitive text").check();
  await page
    .getByLabel("Text for the selected field")
    .fill("human-only-secret");
  await page.getByRole("button", { name: "Send text", exact: true }).click();
  await expect(page.getByLabel("Text for the selected field")).toHaveValue("");
  await expect
    .poll(() =>
      actions.some(
        (a) => a.action === "type" && a.text === "human-only-secret",
      ),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Enter", exact: true }).click();
  await expect
    .poll(() => actions.some((a) => a.action === "key" && a.text === "Enter"))
    .toBe(true);
  await page
    .getByRole("button", { name: "Save this page", exact: true })
    .click();
  await expect(page.getByText("Saved browser copy")).toBeVisible();
  await page
    .getByRole("button", { name: "Return to saved item", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect.poll(() => actions.some((a) => a.action === "close")).toBe(true);
  const previous = gets;
  await page.waitForTimeout(2300);
  expect(gets).toBe(previous);
  expect(actions.filter((a) => a.action === "start")).toHaveLength(1);
  console.log(
    "PASS mobile takeover, coordinate mapping, scroll, ephemeral password input, keys, save, close, stopped polling, single AI start",
  );
} finally {
  await browser.close();
}
