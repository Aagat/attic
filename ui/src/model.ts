export type CaptureStatus =
  | "Preserved"
  | "Partial copy"
  | "Capture failed"
  | "Preserving"
  | "Original PDF";
export type Delivery =
  | "Not requested"
  | "Preparing document"
  | "Email accepted"
  | "Preparation failed"
  | "Delivery failed"
  | "Outcome uncertain";
export interface Item {
  jobId?: string;
  hasPdf?: boolean;
  pdfStatus?: string;
  indexStatus?: string;
  suggestedTags?: string[];
  enrichmentStatus?: string;
  textAvailable?: boolean;
  id: string;
  title: string;
  url: string;
  kind: "Article" | "Reference" | "PDF";
  source: string;
  excerpt: string;
  tags: string[];
  notes: string;
  saved: string;
  capture: CaptureStatus;
  delivery: Delivery;
  author: string;
  folder?: string;
  pages?: number;
  fileId?: string;
  versions: {
    id?: string;
    missing?: string[];
    date: string;
    status: "Complete" | "Partial";
  }[];
}
const examples: Partial<Item>[] = [
  {
    title: "The unreasonable effectiveness of simple systems",
    source: "worksinprogress.co",
    url: "https://worksinprogress.co/issue/simple-systems",
    excerpt:
      "Why the most durable tools often begin with fewer assumptions — and how restraint compounds over time.",
    author: "Maya Chen",
    tags: ["architecture", "reliability", "systems"],
  },
  {
    title:
      "Designing Data-Intensive Applications: The Trouble with Distributed Systems and Why Partial Failure Changes Everything",
    source: "martin.kleppmann.com",
    excerpt:
      "…in a distributed system, partial failure is nondeterministic. You may not even know whether a request succeeded…",
    author: "Martin Kleppmann",
    tags: ["distributed systems", "architecture"],
  },
  {
    title:
      "Notes on reliable queues, delivery semantics, and the uncomfortable gap between ‘accepted’ and ‘arrived’",
    source: "brandur.org",
    excerpt:
      "Email delivery gives us an especially useful example: relay acceptance does not confirm delivery to a downstream device…",
    tags: ["reliability", "distributed systems"],
    delivery: "Email accepted",
  },
  {
    title:
      "Jepsen: On the Perils of Network Partitions, Clocks, and Apparently Successful Operations",
    source: "jepsen.io",
    kind: "Reference",
    excerpt:
      "A client timeout does not tell you whether the operation took effect. Retrying can produce a duplicate unless…",
    capture: "Partial copy",
    tags: ["distributed systems", "research"],
  },
  {
    title: "Fault-Tolerant Distributed Systems — Lecture Notes, Autumn Term",
    source: "cs.cornell.edu",
    kind: "PDF",
    pages: 184,
    capture: "Original PDF",
    excerpt: "PDF · 184 pages · searchable text",
    tags: ["distributed systems", "research"],
  },
  {
    title: "PostgreSQL 18 documentation: Full text search",
    source: "postgresql.org",
    kind: "Reference",
    capture: "Partial copy",
    excerpt:
      "A practical reference for finding a half-remembered phrase across a large collection.",
    tags: ["databases", "reference"],
  },
  {
    title: "A small web is still possible",
    source: "neustadt.fr",
    capture: "Capture failed",
    excerpt: "Keeping the web personal, durable, and worth returning to.",
    tags: ["web", "ideas"],
  },
  {
    title: "Designing Data-Intensive Applications — notes & excerpts",
    source: "Local upload",
    kind: "PDF",
    pages: 186,
    capture: "Original PDF",
    excerpt: "PDF · 186 pages · searchable text",
    tags: ["architecture", "systems"],
    delivery: "Delivery failed",
  },
  {
    title: "The garden and the stream: a technopastoral",
    source: "hapgood.us",
    excerpt:
      "A collection grows through care and curiosity, without the pressure of a feed.",
    tags: ["ideas", "writing"],
  },
  {
    title: "Against an increasingly user-hostile web",
    source: "drewdevault.com",
    capture: "Preserving",
    tags: ["web"],
    excerpt:
      "An archive should still be useful when its original sources have changed.",
  },
  {
    title: "Cómo conservar lo que merece la pena",
    source: "archivo.example",
    excerpt:
      "Una biblioteca personal para las ideas que todavía no sabemos cuándo necesitaremos.",
    tags: ["ideas", "writing"],
  },
  {
    title: "Building reliable software with fewer moving parts",
    source: "danluu.com",
    tags: ["architecture", "reliability"],
    excerpt:
      "What happens when we trade a little flexibility for a system we can understand?",
    delivery: "Outcome uncertain",
  },
];
export const seed: Item[] = examples.map((x, i) => ({
  id: `sample-${i + 1}`,
  kind: "Article",
  title: "",
  url: `https://${x.source}/`,
  source: "",
  excerpt: "",
  tags: [],
  notes:
    i === 0
      ? "Useful explanation of why simple architecture survives growth."
      : "",
  saved: new Date(Date.UTC(2026, 8, 8 - i * 4)).toISOString(),
  capture: "Preserved",
  delivery: "Not requested",
  author: "",
  folder: "Research / Systems / Reliability",
  versions: [
    { date: "2026-08-18T14:32:00Z", status: "Complete" },
    { date: "2026-03-04T10:00:00Z", status: "Partial" },
    { date: "2025-11-11T12:00:00Z", status: "Complete" },
  ],
  ...x,
}));
async function records(
  action: "get" | "put",
  items?: Item[],
): Promise<unknown> {
  const db = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open("attic-preview-records", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("records");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  return new Promise((resolve, reject) => {
    const tx = db.transaction(
      "records",
      action === "get" ? "readonly" : "readwrite",
    );
    const store = tx.objectStore("records");
    const request =
      action === "get" ? store.get("items") : store.put(items, "items");
    tx.oncomplete = () => {
      db.close();
      resolve(request.result);
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
  });
}
export async function loadItems(): Promise<Item[]> {
  const value = await records("get");
  return validItems(value) ? value : seed;
}
export async function saveItems(items: Item[]) {
  await records("put", items);
}
export function validItems(value: unknown): value is Item[] {
  return (
    Array.isArray(value) &&
    value.every(
      (x) =>
        x &&
        typeof x.id === "string" &&
        typeof x.title === "string" &&
        typeof x.url === "string" &&
        typeof x.source === "string" &&
        typeof x.excerpt === "string" &&
        typeof x.notes === "string" &&
        typeof x.author === "string" &&
        typeof x.saved === "string" &&
        Number.isFinite(Date.parse(x.saved)) &&
        ["Article", "Reference", "PDF"].includes(x.kind) &&
        [
          "Preserved",
          "Partial copy",
          "Capture failed",
          "Preserving",
          "Original PDF",
        ].includes(x.capture) &&
        [
          "Not requested",
          "Preparing document",
          "Email accepted",
          "Delivery failed",
          "Outcome uncertain",
        ].includes(x.delivery) &&
        Array.isArray(x.tags) &&
        x.tags.every((t: unknown) => typeof t === "string") &&
        Array.isArray(x.versions) &&
        x.versions.every(
          (v: { date?: string; status?: string }) =>
            v &&
            typeof v.date === "string" &&
            ["Complete", "Partial"].includes(v.status || ""),
        ),
    )
  );
}
export function safeUrl(raw: string) {
  try {
    const url = new URL(raw);
    return ["http:", "https:"].includes(url.protocol) ? url.href : null;
  } catch {
    return null;
  }
}
export const dateLabel = (date: string) =>
  new Date(date).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
export function makeItem(raw: string, send = false): Item {
  const url = new URL(raw);
  url.hash = "";
  return {
    id: crypto.randomUUID(),
    title:
      url.hostname +
      (url.pathname === "/" ? "" : url.pathname.replaceAll("/", " / ")),
    url: url.href,
    source: url.hostname,
    kind: "Article",
    excerpt:
      "Saved in this preview. Capture and indexing will be available after server integration.",
    tags: [],
    notes: "",
    saved: new Date().toISOString(),
    capture: "Preserving",
    delivery: send ? "Preparing document" : "Not requested",
    author: "",
    versions: [],
  };
}
export async function fileStore(
  action: "get" | "put" | "delete",
  id: string,
  file?: File,
): Promise<File | undefined> {
  const payload = file
    ? {
        name: file.name,
        type: file.type,
        lastModified: file.lastModified,
        data: await file.arrayBuffer(),
      }
    : undefined;
  const db = await new Promise<IDBDatabase>((resolve, reject) => {
    const req = indexedDB.open("attic-preview-files", 1);
    req.onupgradeneeded = () => req.result.createObjectStore("files");
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
  return new Promise((resolve, reject) => {
    const tx = db.transaction(
      "files",
      action === "get" ? "readonly" : "readwrite",
    );
    const store = tx.objectStore("files");
    const req =
      action === "get"
        ? store.get(id)
        : action === "put"
          ? store.put(payload, id)
          : store.delete(id);
    tx.oncomplete = () => {
      db.close();
      const result = req.result;
      resolve(
        action === "get" && result
          ? result instanceof File
            ? result
            : new File([result.data], result.name, {
                type: result.type,
                lastModified: result.lastModified,
              })
          : undefined,
      );
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
  });
}
