import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import * as Popover from "@radix-ui/react-popover";
import {
  Search,
  ChevronDown,
  ArrowLeft,
  ArrowRight,
  SlidersHorizontal,
  Archive,
  X,
  Circle,
  Plus,
} from "lucide-react";
import { Badge, Button, input } from "./primitives";
import { SaveForm } from "./SaveForm";
import { dateLabel } from "../model";
import { usePreview } from "../state";
export function Library() {
  const { items, openSave } = usePreview();
  const [params, setParams] = useSearchParams();
  const search = useRef<HTMLInputElement>(null);
  const query = params.get("q") || "",
    kind = params.get("kind") || "",
    source = params.get("source") || "",
    tag = params.get("tag") || "",
    status = params.get("status") || "",
    date = params.get("date") || "";
  const [filterOpen, setFilterOpen] = useState(false);
  const page = Math.max(1, Number(params.get("page")) || 1),
    pageSize = 8;
  function change(key: string, value: string) {
    setParams((p) => {
      if (value) p.set(key, value);
      else p.delete(key);
      p.delete("page");
      return p;
    });
  }
  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        search.current?.focus();
      }
    };
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, []);
  useEffect(() => {
    if (params.get("focus")) search.current?.focus();
  }, [params]);
  const filtered = items.filter(
    (i) =>
      (!kind || i.kind === kind) &&
      (!source || i.source === source) &&
      (!tag || i.tags.includes(tag)) &&
      (!status || i.capture === status) &&
      (!date || i.saved.slice(0, 10) >= date) &&
      query
        .toLowerCase()
        .split(/\s+/)
        .filter(Boolean)
        .every((term) =>
          `${i.title} ${i.source} ${i.excerpt} ${i.notes} ${i.tags.join(" ")}`
            .toLowerCase()
            .includes(term),
        ),
  );
  const pages = Math.max(1, Math.ceil(filtered.length / pageSize)),
    current = Math.min(page, pages),
    visible = filtered.slice((current - 1) * pageSize, current * pageSize);
  const active = !!(query || kind || source || tag || status || date);
  const filterFields = (
    <>
      <label className="grid gap-2 text-xs">
        Source
        <select
          aria-label="Source"
          className={input}
          value={source}
          onChange={(e) => change("source", e.target.value)}
        >
          <option value="">All sources</option>
          {[...new Set(items.map((i) => i.source))].sort().map((s) => (
            <option key={s}>{s}</option>
          ))}
        </select>
      </label>
      <label className="grid gap-2 text-xs">
        Tags
        <select
          aria-label="Tags"
          className={input}
          value={tag}
          onChange={(e) => change("tag", e.target.value)}
        >
          <option value="">All tags</option>
          {[...new Set(items.flatMap((i) => i.tags))].sort().map((s) => (
            <option key={s}>{s}</option>
          ))}
        </select>
      </label>
      <label className="grid gap-2 text-xs">
        Saved since
        <input
          aria-label="Saved since"
          type="date"
          className={input}
          value={date}
          onChange={(e) => change("date", e.target.value)}
        />
      </label>
      <label className="grid gap-2 text-xs">
        Capture status
        <select
          aria-label="Capture status"
          className={input}
          value={status}
          onChange={(e) => change("status", e.target.value)}
        >
          <option value="">All capture states</option>
          {[
            "Preserved",
            "Partial copy",
            "Capture failed",
            "Preserving",
            "Original PDF",
          ].map((s) => (
            <option key={s}>{s}</option>
          ))}
        </select>
      </label>
    </>
  );
  return (
    <main
      id="main"
      className="mx-auto max-w-[1600px] px-5 pb-28 pt-7 sm:px-8 lg:pb-10"
    >
      <div className="mb-6 flex items-end justify-between">
        <div>
          <h1 className="font-display text-4xl">Library</h1>
          <p className="mt-2 text-xs text-[var(--muted)]">
            A home for things you might need later.
          </p>
        </div>
        <span className="hidden text-xs text-[var(--muted)] sm:block">
          {items.length} saved items ·{" "}
          {items.filter((i) => i.capture === "Preserving").length} indexing
        </span>
      </div>
      <div className="flex h-[58px] focus-within:ring-2 focus-within:ring-[var(--accent)] items-center gap-3 rounded border border-[var(--ink)] bg-[var(--paper)] px-4">
        <Search size={19} />
        <input
          ref={search}
          aria-label="Search library"
          className="min-w-0 flex-1 bg-transparent py-3 text-sm outline-none!"
          value={query}
          onChange={(e) => change("q", e.target.value)}
          placeholder="Search titles, notes, or page text"
        />
        {query && (
          <button aria-label="Clear search" onClick={() => change("q", "")}>
            <X size={17} />
          </button>
        )}
        <kbd className="hidden rounded border border-[var(--line)] px-2 py-1 text-[10px] text-[var(--muted)] sm:block">
          ⌘ K
        </kbd>
      </div>
      <div className="my-4 flex flex-wrap items-center gap-2">
        <select
          aria-label="Item type"
          className="min-h-11 rounded bg-[var(--ink)] px-3 text-xs text-[var(--paper)]"
          value={kind}
          onChange={(e) => change("kind", e.target.value)}
        >
          <option value="">All items</option>
          <option>Article</option>
          <option>Reference</option>
          <option>PDF</option>
        </select>
        <Popover.Root open={filterOpen} onOpenChange={setFilterOpen}>
          <Popover.Trigger asChild>
            <Button>
              <SlidersHorizontal size={14} />
              Filters
              {(source || tag || status || date) && (
                <Circle size={7} fill="currentColor" />
              )}
              <ChevronDown size={12} />
            </Button>
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content
              align="start"
              sideOffset={8}
              className="z-30 grid w-[min(330px,calc(100vw-32px))] gap-4 rounded border border-[var(--line)] bg-[var(--paper)] p-5 shadow-lg"
            >
              <div className="flex items-center justify-between">
                <h2 className="font-medium">Refine your library</h2>
                <Popover.Close aria-label="Close filters">
                  <X size={18} />
                </Popover.Close>
              </div>
              {filterFields}
              <Button onClick={() => setFilterOpen(false)}>
                Show {filtered.length} items
              </Button>
            </Popover.Content>
          </Popover.Portal>
        </Popover.Root>
        {[source, tag, status, date].filter(Boolean).map((v) => (
          <Badge key={v}>{v}</Badge>
        ))}
        {active && (
          <button
            className="min-h-11 px-2 text-xs text-[var(--muted)] underline"
            onClick={() => setParams({})}
          >
            Clear all
          </button>
        )}
        <span role="status" className="ml-auto text-xs text-[var(--muted)]">
          {filtered.length} {query ? "results" : "items"}
        </span>
      </div>
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px] xl:grid-cols-[minmax(0,1fr)_350px]">
        <section aria-label="Saved items" className="min-w-0">
          {visible.map((i) => (
            <article
              key={i.id}
              className="group border-b border-[var(--line)] px-1 py-5 transition-colors hover:bg-[var(--paper)] sm:px-3 motion-reduce:transition-none"
            >
              <div className="mb-2 flex items-center gap-3 text-[10px] tracking-wider">
                <span className="font-medium text-[var(--accent-ink)]">
                  {i.kind.toUpperCase()}
                </span>
                <span className="truncate text-[var(--muted)]">{i.source}</span>
              </div>
              <Link
                to={`/items/${i.id}`}
                className="font-display break-words text-[23px] leading-[1.22] group-hover:underline decoration-[var(--line)] underline-offset-4 sm:text-[25px]"
              >
                {i.title}
              </Link>
              <p className="mt-2 line-clamp-2 text-xs leading-5 text-[var(--muted)]">
                {i.excerpt}
              </p>
              <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
                <span className="text-[10px] text-[var(--muted)]">
                  {i.kind === "PDF" ? "Uploaded" : "Saved"} ·{" "}
                  {dateLabel(i.saved)}
                </span>
                <Badge
                  tone={
                    i.capture === "Capture failed"
                      ? "danger"
                      : ["Preserving", "Partial copy"].includes(i.capture)
                        ? "warning"
                        : "success"
                  }
                >
                  <Circle size={5} fill="currentColor" />
                  {i.capture}
                </Badge>
              </div>
            </article>
          ))}
          {!visible.length && (
            <div className="py-20 text-center">
              <Archive className="mx-auto mb-5 size-9 text-[var(--muted)]" />
              <h2 className="font-display text-3xl">
                {active
                  ? "Nothing here matches yet"
                  : "Your archive starts here"}
              </h2>
              <p className="mx-auto my-4 max-w-sm text-sm leading-6 text-[var(--muted)]">
                {active
                  ? "Try a different phrase or broaden your filters. Your saved items are still in your library."
                  : "Save a link or upload a PDF. You can decide how to organize it later."}
              </p>
              <Button onClick={() => (active ? setParams({}) : openSave())}>
                {active ? "Clear search and filters" : "Save your first link"}
              </Button>
            </div>
          )}
          {!!visible.length && (
            <nav
              aria-label="Pagination"
              className="mt-5 flex items-center justify-between gap-3 text-xs"
            >
              <span className="text-[var(--muted)]">
                Showing {(current - 1) * pageSize + 1}–
                {Math.min(current * pageSize, filtered.length)} of{" "}
                {filtered.length}
              </span>
              <div className="flex gap-1">
                <Button
                  aria-label="Previous page"
                  disabled={current === 1}
                  onClick={() =>
                    setParams((p) => {
                      p.set("page", String(current - 1));
                      return p;
                    })
                  }
                >
                  <ArrowLeft size={14} />
                </Button>
                {[...new Set([1, current - 1, current, current + 1, pages])]
                  .filter((p) => p >= 1 && p <= pages)
                  .sort((a, b) => a - b)
                  .map((p) => (
                    <Button
                      key={p}
                      aria-current={p === current ? "page" : undefined}
                      className={
                        p === current
                          ? "bg-[var(--ink)]! text-[var(--paper)]"
                          : ""
                      }
                      onClick={() =>
                        setParams((params) => {
                          params.set("page", String(p));
                          return params;
                        })
                      }
                    >
                      {p}
                    </Button>
                  ))}
                <Button
                  aria-label="Next page"
                  disabled={current === pages}
                  onClick={() =>
                    setParams((p) => {
                      p.set("page", String(current + 1));
                      return p;
                    })
                  }
                >
                  <ArrowRight size={14} />
                </Button>
              </div>
            </nav>
          )}
        </section>
        <aside className="hidden lg:block">
          <div className="sticky top-24">
            <SaveForm sidebar />
          </div>
        </aside>
      </div>
    </main>
  );
}
