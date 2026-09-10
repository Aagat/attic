import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
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
} from "lucide-react";
import { Badge, Button, input } from "./primitives";
import { FilterAutocomplete } from "./FilterAutocomplete";
import { BulkActions } from "./BulkActions";
import { SaveForm } from "./SaveForm";
import { dateLabel } from "../model";
import { useArchive, useArchiveQuery } from "../state";
export function Library() {
  const { archive, openSave, hiddenItems } = useArchive();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const search = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState(() => {
    if (params.has("q")) return params.get("q") || "";
    try {
      return sessionStorage.getItem("attic-library-query") || "";
    } catch {
      return "";
    }
  });
  useEffect(() => {
    if (params.has("q")) {
      setQuery(params.get("q") || "");
      setParams(
        (previous) => {
          previous.delete("q");
          return previous;
        },
        { replace: true },
      );
    }
  }, [params, setParams]);
  useEffect(() => {
    try {
      sessionStorage.setItem("attic-library-query", query);
    } catch {
      /* Search still works without storage. */
    }
  }, [query]);
  const kind = params.get("kind") || "",
    source = params.get("source") || "",
    tag = params.get("tag") || "",
    status = params.get("status") || "",
    date = params.get("date") || "";
  const [selected, setSelected] = useState<string[]>([]);
  const [bulkBusy, setBulkBusy] = useState(false);
  useEffect(() => {
    setSelected([]);
  }, [params.toString(), query]);
  const [filterOpen, setFilterOpen] = useState(false);
  const page = Math.max(1, Number(params.get("page")) || 1),
    pageSize = 8;
  function change(key: string, value: string) {
    if (key === "q") {
      setQuery(value);
      setParams(
        (previous) => {
          previous.delete("page");
          return previous;
        },
        { replace: true },
      );
      return;
    }
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
  const requestParams = new URLSearchParams(params);
  if (query) requestParams.set("q", query);
  const { data, error } = useArchiveQuery(
    "library:" + requestParams.toString(),
    (signal) => archive.list(requestParams, signal),
  );
  const items = data?.items || [];
  const total = data?.total || 0;
  const pages = Math.max(1, Math.ceil(total / pageSize)),
    current = page,
    visible = items.filter((item) => !hiddenItems.includes(item.id));
  function toggle(id: string) {
    if (bulkBusy) return;
    setSelected((previous) =>
      previous.includes(id)
        ? previous.filter((value) => value !== id)
        : [...previous, id],
    );
  }
  const active = !!(query || kind || source || tag || status || date);
  const filterFields = (
    <>
      <FilterAutocomplete
        field="source"
        label="Source"
        value={source}
        onChange={(value) => change("source", value)}
      />
      <FilterAutocomplete
        field="tag"
        label="Tags"
        value={tag}
        onChange={(value) => change("tag", value)}
      />
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
            "Capture failed",
            "Needs browser capture",
            "Not captured",
            "Capture queued",
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
      {error && (
        <p role="alert" className="mb-5 text-sm text-[var(--danger)]">
          {error}
        </p>
      )}
      {!data && !error && <p role="status">Loading library…</p>}
      <div className="mb-6 flex items-end justify-between">
        <div>
          <h1 className="font-display text-4xl">Library</h1>
          <p className="mt-2 text-xs text-[var(--muted)]">
            A home for things you might need later.
          </p>
        </div>
        <span className="hidden text-xs text-[var(--muted)] sm:block">
          {total} matching items{data?.estimated ? " (estimated)" : ""}
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
        <div className="relative inline-flex">
          <select
            aria-label="Item type"
            className="min-h-11 appearance-none rounded bg-[var(--ink)] pl-3 pr-9 text-xs text-[var(--paper)]"
            value={kind}
            onChange={(e) => change("kind", e.target.value)}
          >
            <option value="">All items</option>
            <option>Article</option>
            {archive.preview && <option>Reference</option>}
            <option>PDF</option>
          </select>
          <ChevronDown
            aria-hidden="true"
            size={14}
            className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[var(--paper)]"
          />
        </div>
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
              className="z-30 grid max-h-[var(--radix-popover-content-available-height)] overflow-y-auto w-[min(330px,calc(100vw-32px))] gap-4 rounded border border-[var(--line)] bg-[var(--paper)] p-5 shadow-lg"
            >
              <div className="flex items-center justify-between">
                <h2 className="font-medium">Refine your library</h2>
                <Popover.Close aria-label="Close filters">
                  <X size={18} />
                </Popover.Close>
              </div>
              {filterFields}
              <Button onClick={() => setFilterOpen(false)}>
                Show {total} items
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
            onClick={() => {
              setQuery("");
              setParams({});
            }}
          >
            Clear all
          </button>
        )}
        <span role="status" className="ml-auto text-xs text-[var(--muted)]">
          {total} {query ? "results" : "items"}
        </span>
      </div>
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px] xl:grid-cols-[minmax(0,1fr)_350px]">
        <section aria-label="Saved items" className="min-w-0">
          <span id="row-shortcuts" className="sr-only">
            Space selects or deselects. Enter opens the reader.
          </span>
          {visible.map((i) => (
            <article
              key={i.id}
              tabIndex={0}
              aria-label={`${i.title}${selected.includes(i.id) ? ", selected" : ""}`}
              aria-describedby="row-shortcuts"
              data-selected={selected.includes(i.id)}
              onClick={(event) => {
                if (
                  !(event.target as HTMLElement).closest("a") &&
                  event.detail < 2
                )
                  toggle(i.id);
              }}
              onDoubleClick={(event) => {
                if (!(event.target as HTMLElement).closest("a"))
                  navigate(`/items/${i.id}`);
              }}
              onKeyDown={(event) => {
                if (event.target !== event.currentTarget) return;
                if (event.key === " ") {
                  event.preventDefault();
                  toggle(i.id);
                }
                if (event.key === "Enter") navigate(`/items/${i.id}`);
              }}
              className={`group cursor-pointer border-b border-[var(--line)] px-1 py-5 transition-colors sm:px-3 motion-reduce:transition-none focus-visible:outline-2 focus-visible:outline-[var(--accent)] ${selected.includes(i.id) ? "bg-[var(--accent-soft)] ring-1 ring-inset ring-[var(--accent)]" : "hover:bg-[var(--paper)]"}`}
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
                    ["Capture failed", "Needs browser capture"].includes(
                      i.capture,
                    )
                      ? "danger"
                      : [
                            "Preserving",
                            "Capture queued",
                            "Not captured",
                            "Capture status unavailable",
                          ].includes(i.capture)
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
          {data && !error && !visible.length && (
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
              <Button
                onClick={() => {
                  if (active) {
                    setQuery("");
                    setParams({});
                  } else openSave();
                }}
              >
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
                {Math.min(current * pageSize, total)} of {total}
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
        <aside
          aria-label="Library actions"
          className={`${selected.length ? "" : "hidden"} lg:block`}
        >
          <div className="sticky top-24">
            {selected.length ? (
              <BulkActions
                items={visible}
                selected={selected}
                setSelected={setSelected}
                busy={bulkBusy}
                setBusy={setBulkBusy}
              />
            ) : (
              <SaveForm sidebar />
            )}
          </div>
        </aside>
      </div>
    </main>
  );
}
