import { test, expect } from "@playwright/test";
import { createHTTPArchive } from "../src/archive/http";

test("capture states reflect the server, including items never captured", async () => {
  for (const [status, label] of Object.entries({
    not_captured: "Not captured",
    queued: "Capture queued",
    capturing: "Preserving",
    partial: "Partial copy",
    complete: "Preserved",
    failed: "Capture failed",
    unexpected: "Capture status unavailable",
  })) {
    const archive = createHTTPArchive(
      async () =>
        new Response(
          JSON.stringify({
            id: "saved",
            kind: "bookmark",
            url: "https://example.com",
            capture_status: status,
          }),
        ),
    );
    expect((await archive.get("saved")).capture).toBe(label);
  }
});

test("capture filters use persisted server states", async () => {
  for (const [label, status] of Object.entries({
    "Not captured": "not_captured",
    "Capture queued": "queued",
    Preserving: "capturing",
    "Original PDF": "not_applicable",
  })) {
    let requested = "";
    const archive = createHTTPArchive(async (url) => {
      requested = String(url);
      return new Response(
        JSON.stringify({ items: [], total: 0, storage_bytes: 0 }),
      );
    });
    await archive.list(new URLSearchParams({ status: label }));
    expect(
      new URL(requested, "https://example.com").searchParams.get(
        "capture_status",
      ),
    ).toBe(status);
  }
});
