import { useEffect, useRef, useState } from "react";
import {
  Link,
  NavLink,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import {
  BookOpen,
  Search,
  Settings as SettingsIcon,
  Plus,
  Upload,
  Check,
  X,
  ArrowLeft,
  WifiOff,
} from "lucide-react";
import { Brand, Button, Modal, input } from "./components/primitives";
import { Library } from "./components/Library";
import { SaveForm } from "./components/SaveForm";
import { Reader } from "./components/Reader";
import { SettingsPage, SetupPage, ConnectPage } from "./components/Settings";
import { PreviewContext } from "./state";
import {
  fileStore,
  loadItems,
  makeItem,
  saveItems,
  seed,
  type Item,
} from "./model";
export function App() {
  const [items, setItemState] = useState<Item[]>(seed),
    [ready, setReady] = useState(false),
    [message, setMessage] = useState(""),
    [saveOpen, setSaveOpen] = useState(false),
    [uploadOpen, setUploadOpen] = useState(false),
    [file, setFile] = useState<File | null>(null),
    [uploadError, setUploadError] = useState(""),
    [uploadBusy, setUploadBusy] = useState(false),
    [uploadSend, setUploadSend] = useState(false),
    [offline, setOffline] = useState(!navigator.onLine);
  const location = useLocation(),
    navigate = useNavigate();
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const isReader = location.pathname.startsWith("/items/"),
    isConnect = location.pathname === "/connect";
  function notify(text: string) {
    setMessage(text);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setMessage(""), 6500);
  }
  useEffect(() => () => clearTimeout(timer.current), []);
  const itemsRef = useRef(items);
  const writes = useRef(Promise.resolve());
  function setItems(action: React.SetStateAction<Item[]>): Promise<void> {
    const next =
      typeof action === "function" ? action(itemsRef.current) : action;
    itemsRef.current = next;
    setItemState(next);
    writes.current = writes.current
      .then(() => saveItems(next))
      .catch(() => {
        setMessage(
          "Device storage is full or unavailable. Changes will last only for this session.",
        );
      });
    return writes.current;
  }
  useEffect(() => {
    let active = true;
    loadItems()
      .then((value) => {
        if (active) {
          itemsRef.current = value;
          setItemState(value);
          setReady(true);
          writes.current = saveItems(value).catch(() =>
            setMessage(
              "Device storage is unavailable. Changes will last only for this session.",
            ),
          );
        }
      })
      .catch(() => {
        if (active) {
          setReady(true);
          setMessage(
            "Device storage is unavailable. Changes will last only for this session.",
          );
        }
      });
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    const on = () => setOffline(false),
      off = () => setOffline(true);
    window.addEventListener("online", on);
    window.addEventListener("offline", off);
    return () => {
      window.removeEventListener("online", on);
      window.removeEventListener("offline", off);
    };
  }, []);
  useEffect(() => {
    document.title = `${isReader ? "Reader" : location.pathname === "/settings" ? "Settings" : "Library"} · Attic`;
    window.scrollTo(0, 0);
  }, [location.pathname, isReader]);
  function save(url: string, send: boolean) {
    const fresh = makeItem(url, send),
      existing = items.find((i) => i.url === fresh.url);
    if (existing) {
      notify(
        "Already in your library. Notes and tags kept; no duplicate delivery requested.",
      );
      navigate(`/items/${existing.id}`);
      return existing;
    }
    setItems((p) => [fresh, ...p]);
    notify(
      send
        ? "Saved in this preview. Kindle preparation simulated."
        : "Bookmarked in this preview. Capture is pending.",
    );
    return fresh;
  }
  function send(id: string) {
    const item = items.find((i) => i.id === id);
    if (!item) return;
    if (item.delivery === "Preparing document") {
      notify("This document is already queued.");
      return;
    }
    setItems((p) =>
      p.map((i) =>
        i.id === id ? { ...i, delivery: "Preparing document" } : i,
      ),
    );
    notify("Kindle preparation simulated. No email has been sent.");
  }
  function recapture(id: string) {
    setItems((p) =>
      p.map((i) => (i.id === id ? { ...i, capture: "Preserving" } : i)),
    );
    notify(
      "Fresh capture queued in the preview. Previous copies remain available.",
    );
  }
  async function upload() {
    if (!file) {
      setUploadError("Choose a PDF first.");
      return;
    }
    if (!file.name.toLowerCase().endsWith(".pdf")) {
      setUploadError("Choose a PDF file.");
      return;
    }
    if (file.size > 50 * 1024 * 1024) {
      setUploadError("The local preview supports PDFs up to 50 MB.");
      return;
    }
    setUploadBusy(true);
    try {
      const header = new TextDecoder().decode(
        await file.slice(0, 1024).arrayBuffer(),
      );
      if (!header.includes("%PDF-"))
        throw new Error("This file does not appear to be a PDF.");
      const id = crypto.randomUUID();
      await fileStore("put", id, file);
      const item: Item = {
        id,
        title: file.name.replace(/\.pdf$/i, ""),
        url: "",
        source: "Local upload",
        kind: "PDF",
        excerpt:
          "Original PDF · stored on this device. Text indexing awaits integration.",
        tags: [],
        notes: "",
        saved: new Date().toISOString(),
        capture: "Original PDF",
        delivery: uploadSend ? "Preparing document" : "Not requested",
        author: "",
        versions: [],
        fileId: id,
      };
      setItems((p) => [item, ...p]);
      setUploadOpen(false);
      setFile(null);
      notify("Original PDF kept on this device. Open it in the reader.");
      navigate(`/items/${id}`);
    } catch (e) {
      setUploadError(
        e instanceof Error
          ? e.message
          : "Unable to keep this file on your device.",
      );
    } finally {
      setUploadBusy(false);
    }
  }
  const shared = new URLSearchParams(location.search);
  const sharedUrl = shared.get("url") || shared.get("text") || "";
  if (!ready)
    return (
      <main id="main" className="flex min-h-dvh items-center justify-center">
        <p role="status" className="font-display text-2xl">
          Opening your archive…
        </p>
      </main>
    );
  return (
    <PreviewContext.Provider
      value={{
        items,
        setItems,
        notify,
        save,
        openSave: () => setSaveOpen(true),
        openUpload: () => {
          setFile(null);
          setUploadError("");
          setUploadSend(false);
          setUploadOpen(true);
        },
        send,
        recapture,
      }}
    >
      <a
        href="#main"
        className="sr-only z-50 focus:not-sr-only focus:fixed focus:bg-[var(--paper)] focus:p-4"
      >
        Skip to content
      </a>
      <div className="flex min-h-7 flex-wrap items-center justify-center gap-x-2 bg-[var(--accent-soft)] px-3 py-1 text-center text-[10px] text-[var(--accent-ink)]">
        <span className="font-semibold">UI PREVIEW</span>
        <span>Sample data · changes stay on this device · no emails sent</span>
        {offline && (
          <span className="inline-flex items-center gap-1">
            <WifiOff size={12} />
            Offline preview
          </span>
        )}
      </div>
      {!isConnect && (
        <header className="sticky top-0 z-20 flex min-h-[72px] items-center justify-between gap-3 border-b border-[var(--line)] bg-[var(--paper)] px-5 sm:px-8">
          <div className="flex items-center gap-5">
            {isReader && (
              <Link
                to="/"
                className="inline-flex min-h-11 items-center gap-2 text-xs"
              >
                <ArrowLeft size={16} />
                <span className="hidden sm:inline">Library</span>
              </Link>
            )}
            <Brand />
          </div>
          <div className="flex items-center gap-2">
            <Link
              to="/setup"
              className="mr-4 hidden text-xs text-[var(--muted)] lg:inline"
            >
              Save from anywhere
            </Link>
            <div className="hidden sm:block">
              <Button
                onClick={() => {
                  setFile(null);
                  setUploadError("");
                  setUploadSend(false);
                  setUploadOpen(true);
                }}
              >
                <Upload size={15} />
                Upload PDF
              </Button>
            </div>
            <Button primary onClick={() => setSaveOpen(true)}>
              <Plus size={16} />
              <span className="hidden sm:inline">Save link</span>
              <span className="sm:hidden">Save</span>
            </Button>
            <Link
              aria-label="Settings"
              to="/settings"
              className="ml-1 hidden min-h-11 items-center px-2 md:inline-flex"
            >
              <SettingsIcon size={18} />
            </Link>
          </div>
        </header>
      )}
      <Routes>
        <Route path="/" element={<Library />} />
        <Route path="/items/:id" element={<Reader />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/setup" element={<SetupPage />} />
        <Route path="/connect" element={<ConnectPage />} />
        <Route
          path="/share"
          element={
            <main id="main" className="mx-auto max-w-lg p-6">
              <h1 className="font-display text-3xl">Save to Attic</h1>
              <div className="mt-6">
                <SaveForm initial={sharedUrl} onSaved={() => navigate("/")} />
              </div>
            </main>
          }
        />
        <Route
          path="*"
          element={
            <main id="main" className="p-10 text-center">
              <h1 className="font-display text-3xl">
                This page isn’t in the archive
              </h1>
              <Link to="/" className="mt-5 inline-block underline">
                Return to library
              </Link>
            </main>
          }
        />
      </Routes>
      {!isConnect && (
        <nav
          aria-label="Mobile navigation"
          className="fixed bottom-[max(12px,env(safe-area-inset-bottom))] left-4 right-4 z-30 flex justify-around rounded-full border border-[var(--line)] bg-[var(--paper)] p-1.5 shadow-lg md:hidden"
        >
          {[
            { to: "/", label: "Library", icon: BookOpen },
            { to: "/?focus=1", label: "Search", icon: Search },
            { to: "/settings", label: "Settings", icon: SettingsIcon },
          ].map(({ to, label, icon: Icon }) => (
            <NavLink
              key={label}
              to={to}
              className={() =>
                `flex min-h-12 min-w-20 flex-col items-center justify-center gap-1 rounded-full text-[10px] ${(label === "Library" && location.pathname === "/" && !location.search) || (label === "Search" && location.search) || (label === "Settings" && location.pathname === "/settings") ? "bg-[var(--accent-soft)] text-[var(--accent-ink)]" : "text-[var(--muted)]"}`
              }
            >
              <Icon size={18} />
              {label}
            </NavLink>
          ))}
        </nav>
      )}
      <Modal open={saveOpen} onOpenChange={setSaveOpen} title="Save a link">
        <SaveForm onSaved={() => setSaveOpen(false)} />
        <button
          className="mt-5 text-xs underline"
          onClick={() => {
            setSaveOpen(false);
            setFile(null);
            setUploadOpen(true);
          }}
        >
          Or upload a PDF
        </button>
      </Modal>
      <Modal
        open={uploadOpen}
        onOpenChange={(v) => {
          if (!uploadBusy) setUploadOpen(v);
        }}
        title="Keep a PDF"
        description="Your original file stays unchanged. In this preview, it is stored only on this device."
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void upload();
          }}
          className="grid gap-5"
        >
          <label className="grid gap-4 rounded border border-dashed border-[var(--line)] bg-[var(--surface)] p-7 text-center">
            <Upload className="mx-auto text-[var(--accent)]" />
            <span className="text-sm">Choose a PDF to keep</span>
            <input
              aria-label="PDF file"
              type="file"
              accept="application/pdf,.pdf"
              className="w-full text-xs file:mr-3 file:rounded file:border-0 file:bg-[var(--accent-soft)] file:p-3"
              onChange={(e) => {
                setFile(e.target.files?.[0] || null);
                setUploadError("");
              }}
            />
            <span className="text-xs text-[var(--muted)]">
              Up to 50 MB in this preview
            </span>
          </label>
          <label className="flex items-center gap-3 text-sm">
            <input
              type="checkbox"
              checked={uploadSend}
              onChange={(e) => setUploadSend(e.target.checked)}
            />
            Also Send to Kindle (simulated)
          </label>
          {uploadError && (
            <p role="alert" className="text-sm text-[var(--danger)]">
              {uploadError}
            </p>
          )}
          <Button primary disabled={uploadBusy} type="submit">
            {uploadBusy ? "Keeping PDF…" : "Keep original PDF"}
          </Button>
        </form>
      </Modal>
      <div
        role="status"
        aria-live="polite"
        className="fixed bottom-24 left-1/2 z-[60] w-[calc(100%-32px)] max-w-lg -translate-x-1/2 md:bottom-6"
      >
        {message && (
          <div className="flex items-start gap-3 rounded border border-[var(--line)] bg-[var(--ink)] p-4 text-sm leading-5 text-[var(--paper)] shadow-lg">
            <Check className="mt-0.5 size-4 shrink-0 text-[var(--accent)]" />
            <span className="flex-1">{message}</span>
            <button
              aria-label="Dismiss notification"
              onClick={() => setMessage("")}
            >
              <X size={16} />
            </button>
          </div>
        )}
      </div>
    </PreviewContext.Provider>
  );
}
