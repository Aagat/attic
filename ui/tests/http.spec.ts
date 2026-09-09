import { test, expect } from "@playwright/test";
import { createHTTPArchive } from "../src/archive/http";

test("capture states reflect the server, including items never captured", async () => {
  for (const [status, label] of Object.entries({
    not_captured: "Not captured",
    queued: "Capture queued",
    capturing: "Preserving",
    partial: "Preserved",
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
    Preserved: "preserved",
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

test("PDF preparation alone does not appear as Kindle delivery", async () => {
  for (const status of ["queued", "processing", "failed", "ready"]) {
    const archive = createHTTPArchive(
      async () =>
        new Response(
          JSON.stringify({
            id: "saved",
            kind: "bookmark",
            pdf_status: status,
            delivery_status: "not_requested",
          }),
        ),
    );
    const item = await archive.get("saved");
    expect(item.pdfStatus).toBe(status);
    expect(item.delivery).toBe("Not requested");
  }
});

function keyStore() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) || null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
}

test("remembered bearer token signs in after reload and is cleared on sign out", async () => {
  const storage = keyStore();
  const requests: RequestInit[] = [];
  let validSession = false;
  const transport: typeof fetch = async (_url, options) => {
    requests.push(options!);
    if (options?.method === "POST") validSession = true;
    if (options?.method === "DELETE") validSession = false;
    return options?.method === "DELETE"
      ? new Response(null, { status: 204 })
      : new Response("{}", { status: validSession ? 200 : 401 });
  };
  await createHTTPArchive(transport, storage).session("test-owner-token");
  validSession = false; // Simulate a server restart.
  const reopened = createHTTPArchive(transport, storage);
  await reopened.session();
  expect(requests[1].method).toBeUndefined();
  expect(requests[2].method).toBe("POST");
  expect(new Headers(requests[2].headers).get("Authorization")).toBe(
    "Bearer test-owner-token",
  );
  await reopened.signOut();
  await expect(
    createHTTPArchive(transport, storage).session(),
  ).rejects.toMatchObject({ status: 401 });
  expect(requests[5].method).toBeUndefined();
  expect(new Headers(requests[5].headers).has("Authorization")).toBe(false);
});

test("expired session reconnects once and retries the same mutation", async () => {
  const storage = keyStore();
  let validSession = true,
    logins = 0;
  const keys: (string | null)[] = [];
  const transport: typeof fetch = async (url, options) => {
    if (String(url).endsWith("/session")) {
      logins++;
      validSession = true;
      return new Response("{}");
    }
    keys.push(new Headers(options?.headers).get("Idempotency-Key"));
    return new Response("{}", { status: validSession ? 202 : 401 });
  };
  const archive = createHTTPArchive(transport, storage);
  await archive.session("test-owner-token");
  validSession = false;
  await archive.act("item", "generate", "one-attempt");
  expect(logins).toBe(2);
  expect(keys).toEqual(["one-attempt", "one-attempt"]);
});

test("rejected remembered token is forgotten without endless retries", async () => {
  const storage = keyStore();
  await createHTTPArchive(async () => new Response("{}"), storage).session(
    "invalidated-token",
  );
  let calls = 0;
  const archive = createHTTPArchive(async () => {
    calls++;
    return new Response("{}", { status: 401 });
  }, storage);
  await expect(archive.get("item")).rejects.toMatchObject({ status: 401 });
  expect(calls).toBe(2);
  const subsequent: RequestInit[] = [];
  await createHTTPArchive(async (_url, init) => {
    subsequent.push(init!);
    return new Response("{}");
  }, storage).session();
  expect(subsequent[0].method).toBeUndefined();
});
