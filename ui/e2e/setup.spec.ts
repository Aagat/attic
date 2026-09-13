import { test, expect } from "@playwright/test";

test.use({ serviceWorkers: "block" });

test("Settings connects ChatGPT after reload and requires explicit mail actions", async ({
  page,
}) => {
  let waiting = false,
    mailTests = 0,
    sends = 0;
  const mail = {
    enabled: false,
    host: "",
    port: 587,
    tls_mode: "starttls",
    username: "",
    sender: "",
    destination: "",
    password_set: false,
    managed: false,
  };
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    let data: unknown = {
      items: [],
      total: 0,
      storage_bytes: 0,
      kindle_configured: false,
      search_configured: false,
    };
    if (path === "/api/v1/setup")
      data = {
        provider: "chatgpt",
        ai: "not_connected",
        login: waiting
          ? {
              state: "waiting",
              url: "https://auth.openai.com/codex/device",
              code: "SAFE-CODE",
            }
          : { state: "idle" },
        mail,
        compatibility: "",
        search_configured: false,
      };
    if (path === "/api/v1/setup/chatgpt") {
      waiting = true;
      data = { state: "starting" };
    }
    if (path === "/api/v1/setup/mail") {
      Object.assign(mail, req.postDataJSON());
      data = mail;
    }
    if (path === "/api/v1/setup/mail/test") {
      mailTests++;
      data = {
        message:
          "SMTP connection and authentication succeeded. No email was sent.",
      };
    }
    if (path === "/api/v1/setup/mail/send-test") {
      sends++;
      expect(req.postDataJSON().destination).toBe("reader@kindle.test");
      data = {
        message:
          "SMTP accepted the test email. Device receipt is not confirmed.",
      };
    }
    await route.fulfill({ json: data });
  });
  await page.goto("/settings");
  await expect(page.getByLabel("SMTP host")).toHaveCount(0);
  await expect(page.getByText("Authorization: idle")).toHaveCount(0);
  await expect(page.getByText("0 saved items", { exact: true })).toBeVisible();
  await page.screenshot({
    path: test.info().outputPath("settings.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Set up extension", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Chromium extension", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Enable Developer mode", { exact: false }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Close dialog" }).click();
  await page
    .getByRole("button", { name: "Manage ChatGPT", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "ChatGPT", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Connect ChatGPT", exact: true })
    .click();
  await expect(page.getByText("SAFE-CODE", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText("SAFE-CODE", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Close dialog" }).click();
  await expect(page.getByText("SAFE-CODE", { exact: true })).toHaveCount(0);
  await page
    .getByRole("button", { name: "Manage delivery", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Kindle delivery", exact: true }),
  ).toBeVisible();
  await page.getByLabel("SMTP host").fill("smtp.example.test");
  await page.getByLabel("Sender email").fill("owner@example.test");
  await page.getByLabel("Kindle recipient").fill("reader@kindle.test");
  await page.getByLabel("Enable explicit delivery requests").check();
  await page
    .getByRole("button", { name: "Save mail settings", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Send test email to reader@kindle.test" }),
  ).toBeEnabled();
  expect(sends).toBe(0);
  expect(mailTests).toBe(0);
  await page
    .getByRole("button", { name: "Test connection (no mail)", exact: true })
    .click();
  await expect(page.getByRole("dialog").getByRole("status")).toContainText(
    "No email was sent",
  );
  expect(sends).toBe(0);
  await page
    .getByRole("button", { name: "Send test email to reader@kindle.test" })
    .click();
  await expect(page.getByRole("dialog").getByRole("status")).toContainText(
    "Device receipt is not confirmed",
  );
  expect(sends).toBe(1);
  expect(
    await page
      .getByRole("dialog")
      .evaluate((e) => e.scrollWidth <= e.clientWidth),
  ).toBe(true);
  await page.screenshot({
    path: test.info().outputPath("setup.png"),
    fullPage: true,
  });
});
