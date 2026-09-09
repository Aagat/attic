import type { Archive, LibraryPage, RecoverySession } from "./contract";
import { ArchiveError } from "./contract";
import type { Item } from "../model";
interface WireItem {
  id: string;
  kind: string;
  url: string;
  title: string;
  notes: string;
  tags: string[];
  folders: string[];
  saved_at: string;
  capture_status: string;
  pdf_status: string;
  delivery_status: string;
  index_status: string;
  enrichment_status: string;
  has_pdf: boolean;
  job_id?: string;
  snippet?: string;
  text_available: boolean;
  suggested_tags: string[];
  classification: string;
  captures?: {
    id: string;
    created_at: string;
    status: string;
    missing: string[];
    source?: "browser" | "server";
  }[];
}
const captures: Record<string, Item["capture"]> = {
  complete: "Preserved",
  partial: "Preserved",
  failed: "Capture failed",
  blocked: "Needs browser capture",
  not_captured: "Not captured",
  queued: "Capture queued",
  capturing: "Preserving",
};
const deliveries: Record<string, Item["delivery"]> = {
  accepted: "Email accepted",
  sent: "Email accepted",
  failed: "Delivery failed",
  uncertain: "Outcome uncertain",
  unconfirmed: "Outcome uncertain",
  pending: "Preparing document",
  queued: "Preparing document",
  sending: "Preparing document",
  retrying: "Preparing document",
};
function item(w: WireItem): Item {
  let source = "Uploaded PDF";
  try {
    source = new URL(w.url).hostname;
  } catch {}
  return {
    id: w.id,
    title: w.title || w.url || "Untitled document",
    url: w.url,
    source,
    kind: w.kind === "pdf" ? "PDF" : "Article",
    notes: w.notes || "",
    tags: w.tags || [],
    author: "",
    saved: w.saved_at,
    excerpt: w.snippet || w.notes || "",
    capture:
      w.kind === "pdf"
        ? "Original PDF"
        : captures[w.capture_status] || "Capture status unavailable",
    delivery:
      w.pdf_status === "failed" && w.delivery_status !== "not_requested"
        ? "Preparation failed"
        : ["queued", "processing"].includes(w.pdf_status) &&
            w.delivery_status !== "not_requested"
          ? "Preparing document"
          : deliveries[w.delivery_status] || "Not requested",
    folder: (w.folders || []).join(" / "),
    versions: (w.captures || [])
      .filter((c) => ["complete", "partial"].includes(c.status))
      .map((c) => ({
        id: c.id,
        date: c.created_at,
        status: c.status === "partial" ? "Partial" : "Complete",
        missing: c.missing || [],
        source: c.source,
      })),
    jobId: w.job_id,
    hasPdf: w.has_pdf,
    pdfStatus: w.pdf_status,
    indexStatus: w.index_status,
    suggestedTags: w.suggested_tags || [],
    enrichmentStatus: w.enrichment_status,
    textAvailable: w.text_available,
  };
}
type KeyStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;
const accessKeyStorage = "attic-access-key";
export function createHTTPArchive(
  transport: typeof fetch = fetch,
  storage?: KeyStorage,
): Archive {
  let rememberedKey = "";
  try {
    storage ??= globalThis.localStorage;
    rememberedKey = storage?.getItem(accessKeyStorage) || "";
  } catch {
    /* Browser storage can be disabled. Keep this tab usable. */
  }
  function remember(key: string) {
    rememberedKey = key;
    try {
      if (key) storage?.setItem(accessKeyStorage, key);
      else storage?.removeItem(accessKeyStorage);
    } catch {
      /* This tab can still authenticate without persistent storage. */
    }
  }
  let reconnecting: Promise<void> | undefined;
  async function signIn(key: string) {
    await request("/session", {
      method: "POST",
      headers: { Authorization: "Bearer " + key },
    });
    remember(key);
  }

  async function request<T>(
    path: string,
    options: RequestInit = {},
    format: "json" | "blob" = "json",
  ): Promise<T> {
    let response: Response;
    try {
      const init = {
        ...options,
        credentials: "same-origin",
        cache: "no-store",
        headers: {
          ...(options.body && !(options.body instanceof FormData)
            ? { "Content-Type": "application/json" }
            : {}),
          ...options.headers,
        },
      } satisfies RequestInit;
      response = await transport("/api/v1" + path, init);
      if (
        response.status === 401 &&
        (path !== "/session" || !options.method) &&
        rememberedKey
      ) {
        // Authentication rejected the request before any mutation ran. Reconnect
        // once, then retry the same body and idempotency key.
        reconnecting ??= signIn(rememberedKey).finally(() => {
          reconnecting = undefined;
        });
        await reconnecting;
        response = await transport("/api/v1" + path, init);
      }
    } catch (error) {
      if (
        error instanceof ArchiveError ||
        (error as Error).name === "AbortError"
      )
        throw error;
      throw new ArchiveError(
        "Cannot reach Attic. Check your connection and try again.",
      );
    }
    if (!response.ok) {
      let message =
        response.status === 422
          ? "A cleaned reading view is unavailable for this capture. Open its original layout."
          : "The request could not be completed.";
      try {
        const body = await response.json();
        message = body.error?.message || message;
      } catch {}
      if (response.status === 401) {
        remember("");
        if (typeof window !== "undefined")
          window.dispatchEvent(new Event("attic-session-expired"));
      }
      throw new ArchiveError(message, response.status);
    }
    if (response.status === 204 || options.method === "HEAD")
      return undefined as T;
    return (
      format === "blob" ? response.blob() : response.json()
    ) as Promise<T>;
  }
  const path = (id: string) => "/items/" + encodeURIComponent(id);
  return {
    preview: false,
    async session(key) {
      if (key) await signIn(key);
      else await request("/session");
    },
    async signOut() {
      await request("/session", { method: "DELETE" });
      remember("");
    },
    async list(p, signal) {
      const q = new URLSearchParams({
        limit: "8",
        offset: String((Math.max(1, Number(p.get("page")) || 1) - 1) * 8),
      });
      for (const [ui, api] of [
        ["q", "q"],
        ["source", "domain"],
        ["tag", "tag"],
        ["date", "from"],
      ])
        if (p.get(ui)) q.set(api, p.get(ui)!);
      if (p.get("kind"))
        q.set("kind", p.get("kind") === "PDF" ? "pdf" : "bookmark");
      const status = p.get("status");
      if (status)
        q.set(
          "capture_status",
          (
            {
              Preserved: "preserved",
              "Partial copy": "partial",
              "Capture failed": "failed",
              "Needs browser capture": "blocked",
              "Not captured": "not_captured",
              "Capture queued": "queued",
              Preserving: "capturing",
              "Original PDF": "not_applicable",
            } as Record<string, string>
          )[status],
        );
      const r = await request<{
        items: WireItem[];
        total: number;
        storage_bytes: number;
        total_estimated: boolean;
      }>("/items?" + q, { signal });
      return {
        items: r.items.map(item),
        total: r.total,
        storageBytes: r.storage_bytes,
        estimated: r.total_estimated,
      } satisfies LibraryPage;
    },
    async recovery(id, action, signal) {
      return request<RecoverySession>(path(id) + "/recovery", {
        method: action ? "POST" : "GET",
        body: action ? JSON.stringify(action) : undefined,
        signal,
      });
    },
    async get(id, signal) {
      return item(await request<WireItem>(path(id), { signal }));
    },
    async save(url, kindle, key) {
      return item(
        await request<WireItem>("/items", {
          method: "POST",
          headers: { "Idempotency-Key": key },
          body: JSON.stringify({ url, action: kindle ? "kindle" : "bookmark" }),
        }),
      );
    },
    async edit(i) {
      await request(path(i.id), {
        method: "PUT",
        body: JSON.stringify({ title: i.title, notes: i.notes, tags: i.tags }),
      });
    },
    async remove(id) {
      await request(path(id), { method: "DELETE" });
    },
    async act(id, action, key) {
      await request(path(id) + "/" + action, {
        method: "POST",
        headers: { "Idempotency-Key": key },
      });
    },
    async upload(file, kindle, key) {
      const body = new FormData();
      body.append("file", file);
      body.append("action", kindle ? "kindle" : "bookmark");
      return item(
        await request<WireItem>("/items/upload", {
          method: "POST",
          body,
          headers: { "Idempotency-Key": key },
        }),
      );
    },
    async transfer(action, file) {
      const body = new FormData();
      body.append("file", file);
      const r = await request<{
        restored?: number;
        imported?: number;
        merged?: number;
        skipped?: number;
        errors?: unknown[];
      }>("/items/" + action, { method: "POST", body });
      return action === "restore"
        ? `${r.restored || 0} records restored. No sites revisited or documents sent.`
        : `${r.imported || 0} imported · ${r.merged || 0} merged · ${r.skipped || 0} skipped · ${r.errors?.length || 0} errors.`;
    },
    async captureURL(i, index, reading, signal) {
      const url = captureURL(i, index, reading);
      if (!url) throw new ArchiveError("No saved copy is available yet.");
      await request(url.replace("/api/v1", ""), { method: "HEAD", signal });
      return url;
    },
    async export() {
      await request("/items/export", { method: "HEAD" });
      return { url: "/api/v1/items/export", filename: "attic.zip" };
    },
    async pdf(i) {
      if (!i.hasPdf || !i.jobId)
        throw new ArchiveError("A reading PDF is not available yet.");
      const blob = await request<Blob>(
        "/jobs/" + encodeURIComponent(i.jobId) + "/artifact",
        {},
        "blob",
      );
      return new File(
        [blob],
        i.title.replace(/[^\p{L}\p{N} ._-]/gu, "_") + ".pdf",
        { type: "application/pdf" },
      );
    },
    async status() {
      const r = await request<{
        total: number;
        storage_bytes: number;
        kindle_configured: boolean;
        search_configured: boolean;
      }>("/items/status");
      return {
        total: r.total,
        storageBytes: r.storage_bytes,
        kindle: r.kindle_configured,
        search: r.search_configured,
      };
    },
  };
}
export function captureURL(item: Item, index: number, reading: boolean) {
  const id = item.versions[index]?.id;
  if (!id) return undefined;
  return `/api/v1/items/${encodeURIComponent(item.id)}/captures/${encodeURIComponent(id)}${reading ? "?view=reader" : ""}`;
}
