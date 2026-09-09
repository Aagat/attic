import { randomId } from "../id";
import type { Archive } from "./contract";
import {
  loadItems,
  saveItems,
  makeItem,
  fileStore,
  seed,
  validItems,
  safeUrl,
  type Item,
} from "../model";
export function createPreviewArchive(): Archive {
  let items: Item[] = structuredClone(seed);
  let loaded = false;
  const ensure = async () => {
    if (!loaded) {
      items = await loadItems();
      await saveItems(items);
      loaded = true;
    }
  };
  const persist = () => saveItems(items);
  return {
    preview: true,
    async session() {
      await ensure();
    },
    async signOut() {},
    async list(p) {
      await ensure();
      const q = (p.get("q") || "").toLowerCase().split(/\s+/);
      const found = items.filter(
        (i) =>
          (!p.get("kind") || i.kind === p.get("kind")) &&
          (!p.get("source") || i.source === p.get("source")) &&
          (!p.get("tag") || i.tags.includes(p.get("tag")!)) &&
          (!p.get("status") || i.capture === p.get("status")) &&
          (!p.get("date") || i.saved.slice(0, 10) >= p.get("date")!) &&
          q.every((t) =>
            `${i.title} ${i.source} ${i.excerpt} ${i.tags} ${i.notes}`
              .toLowerCase()
              .includes(t),
          ),
      );
      const offset = (Math.max(1, Number(p.get("page")) || 1) - 1) * 8;
      return {
        items: found.slice(offset, offset + 8),
        total: found.length,
        storageBytes: 38400000000,
      };
    },
    async recovery() {
      return {
        status: "failed",
        url: "",
        title: "",
        message:
          "Browser recovery is available when connected to your Attic server.",
        width: 1024,
        height: 768,
        screenshot: "",
      };
    },
    async suggestions(field, query) {
      await ensure();
      return [
        ...new Set(
          items.flatMap((item) =>
            field === "tag" ? item.tags : [item.source],
          ),
        ),
      ]
        .filter((value) => value.toLowerCase().includes(query.toLowerCase()))
        .sort()
        .slice(0, 20);
    },
    async get(id) {
      await ensure();
      const i = items.find((i) => i.id === id);
      if (!i) throw new Error("This item is no longer here.");
      return i;
    },
    async save(url, kindle) {
      const fresh = makeItem(url, kindle),
        existing = items.find((i) => i.url === fresh.url);
      if (existing) return existing;
      items = [fresh, ...items];
      await persist();
      return fresh;
    },
    async edit(item) {
      items = items.map((i) => (i.id === item.id ? item : i));
      await persist();
    },
    async remove(id) {
      const item = items.find((i) => i.id === id);
      if (item?.fileId) await fileStore("delete", item.fileId);
      items = items.filter((i) => i.id !== id);
      await persist();
    },
    async act(id, action) {
      items = items.map((i) =>
        i.id === id
          ? {
              ...i,
              ...(action === "send"
                ? { delivery: "Preparing document" as const }
                : action === "generate"
                  ? { pdfStatus: "queued" }
                  : { capture: "Preserving" as const }),
            }
          : i,
      );
      await persist();
    },
    async upload(file, kindle) {
      const id = randomId();
      await fileStore("put", id, file);
      const i: Item = {
        id,
        title: file.name.replace(/\.pdf$/i, ""),
        url: "",
        source: "Local upload",
        kind: "PDF",
        excerpt: "Original PDF · stored on this device.",
        notes: "",
        tags: [],
        author: "",
        saved: new Date().toISOString(),
        capture: "Original PDF",
        delivery: kindle ? "Preparing document" : "Not requested",
        versions: [],
        fileId: id,
      };
      items = [i, ...items];
      await persist();
      return i;
    },
    async transfer(action, file) {
      const text = await file.text();
      let incoming: Item[];
      if (action === "restore") {
        const data = JSON.parse(text);
        if (!validItems(data.items))
          throw new Error("Choose a preview JSON export.");
        incoming = data.items;
      } else {
        incoming = [
          ...new DOMParser()
            .parseFromString(text, "text/html")
            .querySelectorAll("a[href]"),
        ].flatMap((a) => {
          const url = safeUrl(a.getAttribute("href") || "");
          return url ? [{ ...makeItem(url), title: a.textContent || url }] : [];
        });
      }
      let count = 0;
      for (const i of incoming) {
        if (
          !items.some((old) => old.id === i.id || (i.url && old.url === i.url))
        ) {
          items.push(i);
          count++;
        }
      }
      await persist();
      return `${count} added · ${incoming.length - count} merged.`;
    },
    async captureURL() {
      throw new Error("Preview captures are samples.");
    },
    async editor() {
      throw new Error("Editing requires a connected Attic server.");
    },
    async saveReading() {
      throw new Error("Editing requires a connected Attic server.");
    },
    async translate() {
      throw new Error("Translation requires a connected Attic server.");
    },
    async readingURL() {
      throw new Error("Preview reading versions are samples.");
    },
    async export() {
      const url = URL.createObjectURL(
        new Blob([JSON.stringify({ format: "attic-ui-preview-v1", items })], {
          type: "application/json",
        }),
      );
      return {
        url,
        filename: "attic-ui-preview.json",
        release: () => URL.revokeObjectURL(url),
      };
    },
    async pdf(i) {
      const file = i.fileId ? await fileStore("get", i.fileId) : undefined;
      if (!file) throw new Error("Upload a PDF to try the reader.");
      return file;
    },
    async status() {
      await ensure();
      return {
        total: items.length,
        storageBytes: 38400000000,
        kindle: true,
        search: true,
      };
    },
    async reset(next) {
      items = next;
      await persist();
    },
  };
}
