import { test, expect } from "@playwright/test";

test("reading edits preserve structure, undo removals, and save without email", async ({
  page,
}) => {
  let html =
    '<p>Keep <strong>bold words</strong></p><p>Newsletter signup</p><pre><code>print("hello")</code></pre><table><tr><td>Cell</td></tr></table><img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7" alt="Diagram">';
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
  await editor.getByText("bold words").click();
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
  await editor.getByText("Newsletter signup").click();
  await page
    .getByRole("button", { name: "Hide selected", exact: true })
    .click();
  await expect(editor.getByText("Newsletter signup")).toHaveCount(0);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await editor.getByText("Newsletter signup").click();
  await page
    .getByRole("button", { name: "Hide selected", exact: true })
    .click();
  await editor.getByText("bold words").click();
  await page
    .getByRole("textbox", { name: "Selected text" })
    .fill("clean words");
  await page.getByRole("button", { name: "Apply text", exact: true }).click();
  await expect(editor.locator("strong")).toHaveText("clean words");
  await page.getByRole("button", { name: "Save and generate PDF" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(html).not.toContain("Newsletter signup");
  expect(html).not.toContain("data-attic-selected");
  expect(html).toContain("<strong>clean words</strong>");
  expect(html).toContain('<pre><code>print("hello")</code></pre>');
  expect(html).toContain("<td>Cell</td>");
  expect(html).toContain('alt="Diagram"');
  expect(writes).toEqual(["/api/v1/items/saved/editor"]);
  await expect(
    page
      .frameLocator('iframe[title="Saved reading version"]')
      .getByText("clean words"),
  ).toBeVisible();
});
