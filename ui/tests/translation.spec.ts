import { test, expect } from "@playwright/test";
import { createHTTPArchive } from "../src/archive/http";

test("translation selects a reading edition without requesting email", async () => {
  const requests: { url: string; init?: RequestInit }[] = [];
  const archive = createHTTPArchive(async (url, init) => {
    requests.push({ url: String(url), init });
    const language = init?.body ? JSON.parse(String(init.body)).language : "en";
    return new Response(
      JSON.stringify({
        id: "saved",
        output_language: language,
        reading_available: true,
        job_id: "translated",
        pdf_status: "ready",
        delivery_status: "not_requested",
      }),
    );
  });
  const translated = await archive.translate("saved", "en");
  expect(translated.outputLanguage).toBe("en");
  expect(translated.readingAvailable).toBe(true);
  expect(translated.delivery).toBe("Not requested");
  expect(requests[0].url).toBe("/api/v1/items/saved/translate");
  expect(JSON.parse(String(requests[0].init?.body))).toEqual({
    language: "en",
  });
  const controller = new AbortController();
  expect(await archive.readingURL(translated, controller.signal)).toBe(
    "/api/v1/items/saved/reading?job=translated",
  );
  expect(requests[1].init?.method).toBe("HEAD");
  expect(requests[1].init?.signal).toBe(controller.signal);
  expect((await archive.translate("saved", "")).outputLanguage).toBe("");
});

