import { test, expect, type Locator } from "@playwright/test";
import { readFileSync } from "node:fs";

// Service-worker fetches bypass Playwright routes, including the mocked session.
test.use({ serviceWorkers: "block" });

test("reading edits preserve structure, undo removals, and save without email", async ({
  page,
  isMobile,
}) => {
  const point = async (locator: Locator) => {
    await locator.scrollIntoViewIfNeeded();
    const box = (await locator.boundingBox())!;
    const viewport = page.viewportSize()!;
    return {
      x:
        Math.max(0, box.x) +
        (Math.min(viewport.width, box.x + box.width) - Math.max(0, box.x)) / 2,
      y:
        Math.max(80, box.y) +
        (Math.min(viewport.height, box.y + box.height) - Math.max(80, box.y)) /
          2,
    };
  };
  const select = async (locator: Locator) => {
    const { x, y } = await point(locator);
    if (isMobile) await page.touchscreen.tap(x, y);
    else await page.mouse.click(x, y);
  };
  const frontend = readFileSync(
    new URL("../../internal/httpapi/frontend.go", import.meta.url),
    "utf8",
  );
  const csp = frontend.match(/Set\("Content-Security-Policy", "([^"]+)"/)![1];
  await page.route("**/items/saved", async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      headers: { ...response.headers(), "content-security-policy": csp },
    });
  });
  let html =
    '<p>Keep <strong>bold words</strong></p><p>Newsletter signup</p><pre><code>print("hello")</code></pre><table><tr><td>Cell</td></tr></table><img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7" alt="Diagram">';
  html += `<p>${"Long preview paragraph. ".repeat(300)}</p>`;
  let item = {
    id: "saved",
    kind: "bookmark",
    title: "Article",
    url: "https://example.com/story",
    saved_at: "2026-09-10T12:00:00Z",
    capture_status: "complete",
    text_available: true,
    delivery_status: "not_requested",
    pdf_status: "not_requested",
    reading_available: false,
    job_id: "",
    captures: [
      {
        id: "capture",
        created_at: "2026-09-10T12:00:00Z",
        status: "complete",
        missing: [],
      },
    ],
  };
  const writes: string[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (["POST", "PUT"].includes(request.method())) writes.push(path);
    if (path.endsWith("/editor")) {
      if (request.method() === "GET")
        return route.fulfill({ json: { html, revision: "original" } });
      const body = request.postDataJSON();
      expect(body.revision).toBe("original");
      html = body.html;
      item = {
        ...item,
        reading_available: true,
        pdf_status: "queued",
        job_id: "edited",
      };
      return route.fulfill({ json: item });
    }
    if (path.endsWith("/reading") || path.includes("/captures/"))
      return route.fulfill({ contentType: "text/html", body: html });
    return route.fulfill({ json: path === "/api/v1/session" ? {} : item });
  });
  await page.goto("/items/saved");
  await page
    .getByRole("button", { name: "Edit reading version", exact: true })
    .click();
  const editor = page.frameLocator('iframe[title="Reading editor"]');
  await expect(page.locator('iframe[title="Reading editor"]')).toHaveAttribute(
    "sandbox",
    "allow-same-origin",
  );
  const frameHeight = () =>
    page
      .locator('iframe[title="Reading editor"]')
      .evaluate((frame) => frame.clientHeight);
  await expect.poll(frameHeight).toBeGreaterThan(1000);
  const fullHeight = await frameHeight();
  await select(editor.locator("p").last());
  await page
    .getByRole("button", { name: "Hide selected", exact: true })
    .click();
  await expect.poll(frameHeight).toBeLessThan(fullHeight - 500);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect.poll(frameHeight).toBeGreaterThan(1000);
  await expect
    .poll(() =>
      page.locator('iframe[title="Reading editor"]').evaluate((frame) => {
        const document = (frame as HTMLIFrameElement).contentDocument!;
        return document.documentElement.scrollHeight <= frame.clientHeight;
      }),
    )
    .toBe(true);
  const hoverPoint = await point(editor.locator("strong"));
  await page.mouse.move(hoverPoint.x, hoverPoint.y);
  await expect(editor.locator("strong")).toHaveAttribute(
    "data-attic-hover",
    "",
  );
  await expect(editor.locator("strong")).toHaveCSS("outline-width", "2px");
  await expect(editor.locator("strong")).toHaveCSS("outline-style", "dashed");
  await expect(editor.locator("[data-attic-overlay]")).toHaveCSS(
    "position",
    "fixed",
  );
  await expect(editor.locator("[data-attic-overlay]")).toContainText(
    "Bold text",
  );
  await select(editor.getByText("bold words"));
  await expect(editor.locator("strong")).toHaveCSS("outline-style", "solid");
  await expect(editor.locator("strong")).toHaveCSS("outline-width", "2px");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const controls = page.getByRole("toolbar", {
    name: "Selected element actions",
  });
  await expect(controls).toBeVisible();
  await expect(
    page.getByRole("tab", { name: "Original layout" }),
  ).toBeDisabled();
  await expect
    .poll(async () => {
      const element = await editor.locator("strong").boundingBox();
      const toolbar = await controls.boundingBox();
      if (!element || !toolbar) return false;
      return (
        toolbar.y + toolbar.height <= element.y ||
        toolbar.y >= element.y + element.height
      );
    })
    .toBe(true);
  await page
    .getByRole("button", { name: "Select parent", exact: true })
    .click();
  await expect(editor.locator("p").first()).toHaveAttribute(
    "data-attic-selected",
    "",
  );
  await select(editor.getByText("bold words"));
  await page
    .getByRole("button", { name: "Select parent", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Hide selected", exact: true })
    .click();
  await expect(editor.getByText("Keep", { exact: false })).toHaveCount(0);
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(writes).toEqual([]);
  await page
    .getByRole("button", { name: "Edit reading version", exact: true })
    .click();
  await expect(editor.locator("strong")).toHaveText("bold words");
  await select(editor.getByText("Newsletter signup"));
  await page
    .getByRole("button", { name: "Hide selected", exact: true })
    .click();
  await expect(editor.getByText("Newsletter signup")).toHaveCount(0);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await select(editor.getByText("Newsletter signup"));
  await page
    .getByRole("button", { name: "Hide selected", exact: true })
    .click();
  await select(editor.getByText("bold words"));
  await page.getByRole("button", { name: "Edit text", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Selected text" })
    .fill("clean words");
  await page.getByRole("button", { name: "Apply text", exact: true }).click();
  await expect(editor.locator("strong")).toHaveText("clean words");
  await page.getByRole("button", { name: "Save and generate PDF" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(html).not.toContain("Newsletter signup");
  expect(html).not.toContain("data-attic-");
  expect(html).toContain("<strong>clean words</strong>");
  expect(html).toContain('<pre><code>print("hello")</code></pre>');
  expect(html).toContain("<td>Cell</td>");
  expect(html).toContain('alt="Diagram"');
  expect(writes).toEqual(["/api/v1/items/saved/editor"]);
  await expect
    .poll(() =>
      page
        .locator('iframe[title="Saved reading version"]')
        .evaluate((frame) => frame.clientHeight),
    )
    .toBeGreaterThan(1000);
  await expect(
    page
      .frameLocator('iframe[title="Saved reading version"]')
      .getByText("clean words"),
  ).toBeVisible();
});
