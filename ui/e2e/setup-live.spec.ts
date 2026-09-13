import { test, expect } from "@playwright/test";

// Runs only against compose.ui-e2e.yaml: the recipient is a local Mailpit
// fixture, and no OAuth or external provider request is made.
test("real Settings preserve mail intent and report runtime readiness", async ({
  page,
  request,
}) => {
  await page.goto("/settings");
  await page.getByLabel("Access key").fill("attic-e2e-owner-key");
  await page
    .getByRole("button", { name: "Connect to Attic", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Settings", exact: true }),
  ).toBeVisible();
  const headers = { Authorization: "Bearer attic-e2e-owner-key" };
  const setup = await (await request.get("/api/v1/setup", { headers })).json();
  const count = async () =>
    (await (await request.get("http://mailpit:8025/api/v1/messages")).json())
      .total;
  const before = await count();
  await expect(page.getByLabel("SMTP host")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Manage delivery", exact: true })
    .click();
  if (setup.mail.managed) {
    await expect(page.getByLabel("SMTP host")).toBeDisabled();
    expect(
      (await request.put("/api/v1/setup/mail", { headers, data: {} })).status(),
    ).toBe(400);
  } else {
    await page.getByLabel("SMTP host").fill("mailpit");
    await page.getByLabel("SMTP port").fill("1025");
    await page.getByLabel("TLS mode").selectOption("none");
    await page.getByLabel("Sender email").fill("attic@example.test");
    await page.getByLabel("Kindle recipient").fill("reader@example.test");
    await page.getByLabel("Enable explicit delivery requests").check();
    await page
      .getByRole("button", { name: "Save mail settings", exact: true })
      .click();
    await expect(page.getByRole("dialog").getByRole("status")).toContainText(
      "No email was sent",
    );
    await page.reload();
    await expect(page.getByLabel("SMTP host")).toHaveValue("mailpit");
    await expect(page.getByLabel("Kindle recipient")).toHaveValue(
      "reader@example.test",
    );
  }
  expect(await count()).toBe(before);
  await page
    .getByRole("button", { name: "Test connection (no mail)", exact: true })
    .click();
  await expect(page.getByRole("dialog").getByRole("status")).toContainText(
    "No email was sent",
  );
  expect(await count()).toBe(before);
  expect(
    (
      await request.post("/api/v1/setup/mail/send-test", {
        headers,
        data: { destination: "different@example.test" },
      })
    ).status(),
  ).toBe(409);
  expect(await count()).toBe(before);
  await page
    .getByRole("button", {
      name: "Send test email to reader@example.test",
      exact: true,
    })
    .click();
  await expect(page.getByRole("dialog").getByRole("status")).toContainText(
    "Device receipt is not confirmed",
  );
  await expect.poll(count).toBe(before + 1);
  await page.getByRole("button", { name: "Close dialog" }).click();
  await page.getByRole("button", { name: "View runtime", exact: true }).click();
  await page
    .getByRole("button", { name: "Check runtime", exact: true })
    .click();
  await expect(
    page.getByText(/Attic PDF template and quality validation passed/),
  ).toBeVisible();
  await expect(page.getByText(/Sandboxed browser launch passed/)).toBeVisible();
  await page.reload();
  await expect(
    page.getByText(/Attic PDF template and quality validation passed/),
  ).toBeVisible();
  const saved = await (await request.get("/api/v1/setup", { headers })).json();
  expect(saved.mail).not.toHaveProperty("password");
});
