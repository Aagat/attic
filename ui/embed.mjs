import { cp, readdir, rm } from "node:fs/promises";
const target = new URL("../internal/httpapi/frontend/", import.meta.url);
for (const name of (await readdir(target)).filter(
  (name) => name !== "README.txt",
))
  await rm(new URL(name, target), { recursive: true, force: true });
await cp(new URL("./dist/", import.meta.url), target, { recursive: true });
