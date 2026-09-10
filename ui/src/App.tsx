import { useRemoval } from "./removal";
import { randomId } from "./id";
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
  WifiOff,
} from "lucide-react";
import { Brand, Button, Modal } from "./components/primitives";
import { Library } from "./components/Library";
import { SaveForm } from "./components/SaveForm";
import { Reader } from "./components/Reader";
import { SettingsPage, SetupPage, ConnectPage } from "./components/Settings";
import { ArchiveContext, archive } from "./state";
export function App() {
  const [authenticated, setAuthenticated] = useState(false),
    [revision, setRevision] = useState(0),
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
  const refresh = () => setRevision((r) => r + 1);
  const removal = useRemoval(archive, refresh, notify);
  async function login(key: string) {
    await archive.session(key);
    setAuthenticated(true);
  }
  async function logout() {
    await archive.signOut();
    setAuthenticated(false);
  }
  useEffect(() => {
    const expired = () => setAuthenticated(false);
    window.addEventListener("attic-session-expired", expired);
    archive
      .session()
      .then(() => setAuthenticated(true))
      .catch(() => setAuthenticated(false))
      .finally(() => setReady(true));
    return () => window.removeEventListener("attic-session-expired", expired);
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
  const pendingActions = useRef(new Set<string>());
  const uploadAttempt = useRef({
    file: null as File | null,
    send: false,
    key: "",
  });
  async function save(url: string, send: boolean, key: string) {
    let item;
    try {
      item = await archive.save(url, send, key);
    } finally {
      refresh();
    }
    notify(
      send
        ? "Saved. Kindle preparation requested."
        : "Bookmarked. Capture continues in the background.",
    );
    return item;
  }
  async function action(id: string, name: "send" | "recapture" | "generate") {
    const pendingKey = id + name;
    if (pendingActions.current.has(pendingKey)) return;
    pendingActions.current.add(pendingKey);
    try {
      await archive.act(id, name, randomId());
      refresh();
      notify(
        name === "send"
          ? "Kindle preparation requested."
          : name === "generate"
            ? "PDF generation requested. No email will be sent."
            : "Fresh capture requested. Previous copies remain available.",
      );
    } catch (e) {
      notify((e as Error).message);
    } finally {
      pendingActions.current.delete(pendingKey);
      refresh();
    }
  }
  const send = (id: string) => {
    void action(id, "send");
  };
  const recapture = (id: string) => {
    void action(id, "recapture");
  };
  async function upload() {
    if (!file) {
      setUploadError("Choose a PDF first.");
      return;
    }
    if (!file.name.toLowerCase().endsWith(".pdf")) {
      setUploadError("Choose a PDF file.");
      return;
    }
    if (file.size > 25_000_000) {
      setUploadError("Choose a PDF no larger than 25 MB.");
      return;
    }
    setUploadBusy(true);
    try {
      const header = new TextDecoder().decode(
        await file.slice(0, 1024).arrayBuffer(),
      );
      if (!header.includes("%PDF-"))
        throw new Error("This file does not appear to be a PDF.");
      if (
        uploadAttempt.current.file !== file ||
        uploadAttempt.current.send !== uploadSend
      )
        uploadAttempt.current = {
          file,
          send: uploadSend,
          key: randomId(),
        };
      const item = await archive.upload(
        file,
        uploadSend,
        uploadAttempt.current.key,
      );
      refresh();
      setUploadOpen(false);
      setFile(null);
      notify("Original PDF saved. Open it in the reader.");
      navigate(`/items/${item.id}`);
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
    <ArchiveContext.Provider
      value={{
        archive,
        revision,
        refresh,
        login,
        logout,
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
        remove: removal.remove,
        hiddenItems: removal.hidden,
        generate: (id: string) => {
          void action(id, "generate");
        },
      }}
    >
      {!authenticated ? (
        <ConnectPage />
      ) : (
        <>
          <a
            href="#main"
            className="sr-only z-50 focus:not-sr-only focus:fixed focus:bg-[var(--paper)] focus:p-4"
          >
            Skip to content
          </a>
          {(archive.preview || offline) && (
            <div className="flex min-h-7 flex-wrap items-center justify-center gap-x-2 bg-[var(--accent-soft)] px-3 py-1 text-center text-[10px] text-[var(--accent-ink)]">
              {archive.preview && (
                <span className="font-semibold">UI PREVIEW</span>
              )}
              {archive.preview && (
                <span>
                  Sample data · changes stay on this device · no emails sent
                </span>
              )}
              {offline && (
                <span className="inline-flex items-center gap-1">
                  <WifiOff size={12} />
                  Offline — reconnect to access your archive
                </span>
              )}
            </div>
          )}
          {!isConnect && !isReader && (
            <header className="sticky top-0 z-20 flex min-h-[72px] items-center justify-between gap-3 border-b border-[var(--line)] bg-[var(--paper)] px-5 sm:px-8">
              <div className="flex items-center gap-5">
                <Brand />
              </div>
              <div className="flex items-center gap-2">
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
                    <SaveForm
                      initial={sharedUrl}
                      onSaved={() => navigate("/")}
                    />
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
              className="fixed bottom-[max(12px,env(safe-area-inset-bottom))] left-4 right-4 z-30 flex justify-around rounded-full border border-[var(--line)] bg-[var(--paper)] p-1.5 shadow-lg lg:hidden"
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
            description="Your original file stays unchanged. Attic keeps it in your archive."
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
                <span className="text-xs text-[var(--muted)]">Up to 25 MB</span>
              </label>
              <label className="flex items-center gap-3 text-sm">
                <input
                  type="checkbox"
                  checked={uploadSend}
                  onChange={(e) => setUploadSend(e.target.checked)}
                />
                Also Send to Kindle
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
            {!!removal.pending.length && (
              <div className="mb-2 flex items-center justify-between gap-3 rounded bg-[var(--ink)] p-4 text-sm text-[var(--paper)] shadow-lg">
                <span>{removal.pending.length} item(s) removed</span>
                <button
                  className="min-h-11 px-3 underline"
                  onClick={removal.undo}
                >
                  Undo
                </button>
              </div>
            )}
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
        </>
      )}
    </ArchiveContext.Provider>
  );
}
