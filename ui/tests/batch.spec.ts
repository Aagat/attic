import { test, expect } from "@playwright/test";
import { applyToItems } from "../src/batch";

test("batch failures retain failed identities and continue remaining operations", async () => {
  const called: string[] = [];
  const failed = await applyToItems(["one", "two", "three"], async (id) => {
    called.push(id);
    if (id === "two") throw new Error("Unavailable");
  });
  expect(called).toEqual(["one", "two", "three"]);
  expect(failed).toEqual(["two"]);
  const retried: string[] = [];
  expect(
    await applyToItems(failed, async (id) => {
      retried.push(id);
    }),
  ).toEqual([]);
  expect(retried).toEqual(["two"]);
});
