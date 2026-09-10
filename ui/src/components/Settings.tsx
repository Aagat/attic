import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  Upload,
  Bookmark,
  Archive,
  RotateCcw,
  ArrowLeft,
  Monitor,
  Smartphone,
  Download,
  Send,
} from "lucide-react";
import * as Tabs from "@radix-ui/react-tabs";
import { Badge, Brand, Button, SectionLabel, input } from "./primitives";
import { safeUrl } from "../model";
import { useArchive, useArchiveQuery } from "../state";
export function SettingsPage() {
  const { archive, refresh, openUpload, logout, notify } = useArchive();
  const { data: status, error } = useArchiveQuery("status", () =>
    archive.status(),
  );
  const [busy, setBusy] = useState(false),
    [result, setResult] = useState("");
  const importer = useRef<HTMLInputElement>(null),
    restore = useRef<HTMLInputElement>(null);
  async function transfer(
    file: File | undefined,
    action: "import" | "restore",
  ) {
    if (!file) return;
    setBusy(true);
    try {
      setResult(await archive.transfer(action, file));
      refresh();
    } catch (e) {
      setResult((e as Error).message);
    } finally {
      setBusy(false);
      if (importer.current) importer.current.value = "";
      if (restore.current) restore.current.value = "";
    }
  }
  async function backup() {
    setBusy(true);
    try {
      const download = await archive.export();
      const a = document.createElement("a");
      a.href = download.url;
      a.download = download.filename;
      a.click();
      if (download.release) setTimeout(download.release, 10000);
      setResult(
        "Archive download started. Check your browser downloads for completion.",
      );
    } catch (e) {
      setResult((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main id="main" className="mx-auto max-w-[1600px] px-5 pb-28 pt-8 sm:px-8">
      <h1 className="font-display text-4xl">Settings</h1>
      <p className="mt-3 text-xs leading-6 text-[var(--muted)]">
        Manage storage, imports, backups, and required connections.
      </p>
      {error && <p role="alert">{error}</p>}
      <section className="mt-6 grid gap-7 border border-[var(--line)] bg-[var(--paper)] p-6 md:grid-cols-[220px_1fr_230px]">
        <div>
          <SectionLabel>Stored files</SectionLabel>
          <p className="font-display mt-3 text-4xl">
            {status
              ? `${(status.storageBytes / 1024 / 1024).toFixed(1)} MB`
              : "…"}
          </p>
          <p className="mt-2 text-xs">{status?.total ?? "…"} saved items</p>
        </div>
        <p className="self-center text-xs leading-6 text-[var(--muted)]">
          Nothing is pruned automatically. Remove saved content explicitly from
          its item page. Storage shown includes preserved files.
        </p>
        <div className="space-y-4 text-xs">
          <SectionLabel>Connections</SectionLabel>
          <p>
            Kindle delivery ·{" "}
            {status ? (status.kindle ? "Configured" : "Not configured") : "…"}
          </p>
          <p>
            Search ·{" "}
            {status ? (status.search ? "Configured" : "Not configured") : "…"}
          </p>
          <p className="text-[var(--muted)]">Managed on the server</p>
        </div>
      </section>
      <h2 className="font-display mt-9 mb-6 text-2xl">Import and backup</h2>
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {[
          {
            title: "Upload PDF",
            body: "Keep the original file unchanged. Open it in the reader.",
            label: "Choose PDF",
            icon: Upload,
            action: openUpload,
          },
          {
            title: "Import bookmarks",
            body: "Add a browser HTML export. Existing links merge without replacing notes or tags.",
            label: "Choose HTML file",
            icon: Bookmark,
            action: () => importer.current?.click(),
          },
          {
            title: "Export archive",
            body: archive.preview
              ? "Export local preview records."
              : "Download a ZIP with your records and preserved files.",
            label: "Export archive",
            icon: Archive,
            action: backup,
          },
          {
            title: "Restore archive",
            body: "Merge an Attic archive. Existing records are kept. Restoring does not revisit sites or send documents.",
            label: "Choose archive",
            icon: RotateCcw,
            action: () => restore.current?.click(),
          },
        ].map(({ title, body, label, icon: Icon, action }, index) => (
          <section
            key={title}
            className="flex min-h-[230px] flex-col items-start border border-[var(--line)] bg-[var(--paper)] p-5"
          >
            <Icon size={20} />
            <h3 className="font-display mt-4 text-2xl">{title}</h3>
            <p className="mb-6 mt-3 text-xs leading-6 text-[var(--muted)]">
              {body}
            </p>
            <Button
              primary={index === 0}
              className="mt-auto"
              disabled={busy}
              onClick={action}
            >
              {label}
            </Button>
          </section>
        ))}
      </div>
      <input
        ref={importer}
        aria-label="Import HTML bookmarks"
        type="file"
        accept=".html,.htm"
        hidden
        onChange={(e) => void transfer(e.target.files?.[0], "import")}
      />
      <input
        ref={restore}
        aria-label="Restore archive"
        type="file"
        accept={archive.preview ? ".json" : ".zip"}
        hidden
        onChange={(e) => void transfer(e.target.files?.[0], "restore")}
      />
      {(busy || result) && (
        <p role="status" className="mt-5 p-5 border border-[var(--line)]">
          {busy ? "Processing archive…" : result}
        </p>
      )}
      <section className="mt-8 border border-[var(--line)] bg-[var(--paper)] p-6">
        <h2 className="font-display text-2xl">Chromium extension</h2>
        <p className="mt-3 text-sm leading-7 text-[var(--muted)]">
          Save pages and search your library from the address bar: type <kbd>a</kbd>,
          press Tab, then enter your search. Suggestions come from your Attic server.
        </p>
        <a
          href="/attic-chromium.zip"
          download
          className="mt-4 inline-flex min-h-11 items-center gap-2 rounded border border-[var(--line)] px-4 text-sm"
        >
          <Download size={16} /> Download Chromium extension
        </a>
        <p className="mt-4 text-xs leading-6 text-[var(--muted)]">
          Unzip the download, open <code>chrome://extensions</code>, enable Developer
          mode, then choose Load unpacked and select the attic folder. Connect your
          server and access key in the extension’s settings. To update, replace the
          files in your existing extension folder and click Reload.
        </p>
      </section>
      <section className="mt-8 flex items-center justify-between gap-5 border-y border-[var(--line)] py-6">
        <div>
          <h2 className="font-display text-2xl">Save from anywhere</h2>
          <p className="mt-2 text-xs">
            Your browser, your phone, your everyday tools.
          </p>
        </div>
        <Link to="/setup" className="underline text-xs">
          Set up sharing
        </Link>
      </section>
      {!archive.preview && (
        <Button
          className="mt-8"
          onClick={() => logout().catch((e) => notify(e.message))}
        >
          Sign out
        </Button>
      )}
    </main>
  );
}
interface InstallEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: string }>;
}
export function SetupPage() {
  const { notify, archive } = useArchive();
  const [install, setInstall] = useState<InstallEvent>(),
    [bookmarks, setBookmarks] = useState(false),
    [extensionState, setExtensionState] = useState("Connected");
  useEffect(() => {
    const capture = (e: Event) => {
      e.preventDefault();
      setInstall(e as InstallEvent);
    };
    window.addEventListener("beforeinstallprompt", capture);
    return () => window.removeEventListener("beforeinstallprompt", capture);
  }, []);
  return (
    <main id="main" className="mx-auto max-w-5xl px-5 pb-28 pt-9 sm:px-8">
      <Link
        to="/settings"
        className="inline-flex min-h-11 items-center gap-2 text-xs text-[var(--muted)]"
      >
        <ArrowLeft size={14} />
        Settings
      </Link>
      <h1 className="font-display mt-4 text-4xl">Save from anywhere</h1>
      <p className="mt-3 max-w-xl text-sm leading-7 text-[var(--muted)]">
        Keep what catches your attention. Find it in Attic when you need it.
      </p>
      <Tabs.Root defaultValue="browser" className="mt-8">
        <Tabs.List
          className="flex gap-2 border-b border-[var(--line)] pb-3"
          aria-label="Sharing platform"
        >
          {[
            ["browser", "Browser", Monitor],
            ["android", "Android", Smartphone],
            ["ios", "iPhone & iPad", Smartphone],
          ].map(([value, label, Icon]) => (
            <Tabs.Trigger
              key={String(value)}
              value={String(value)}
              className="flex min-h-11 items-center gap-2 rounded px-3 text-xs data-[state=active]:bg-[var(--accent-soft)] data-[state=active]:text-[var(--accent-ink)]"
            >
              {typeof Icon !== "string" && <Icon size={15} />}
              <span>{String(label)}</span>
            </Tabs.Trigger>
          ))}
        </Tabs.List>
        <Tabs.Content
          value="browser"
          className="mt-7 grid gap-8 md:grid-cols-2"
        >
          <div>
            <h2 className="font-display text-3xl">
              A little room in your browser
            </h2>
            <p className="mt-4 text-sm leading-7 text-[var(--muted)]">
              Use Attic’s Chromium extension to bookmark the current page or
              send it to Kindle. You can also save links from the context menu.
            </p>
            <ol className="mt-6 list-decimal space-y-4 pl-5 text-sm leading-6">
              <li>
                <a href="/attic-chromium.zip" download className="underline">Download the Chromium extension</a>,
                unzip it, then use Load unpacked at <code>chrome://extensions</code>
                with Developer mode enabled.
              </li>
              <li>
                Enter your server address and access key in extension settings.
              </li>
              <li>
                Optionally enable automatic browser bookmarks and grant bookmark
                permission.
              </li>
            </ol>
            <p className="mt-6 rounded bg-[var(--accent-soft)] p-4 text-xs leading-6">
              Removing a browser bookmark never removes its Attic copy. Folder
              information stays separate from your tags and notes.
            </p>
          </div>
          {archive.preview ? (
            <div className="rounded border border-[var(--line)] bg-[var(--paper)] p-6">
              <div className="flex items-center justify-between">
                <Brand />
                <Badge
                  tone={extensionState === "Connected" ? "success" : "warning"}
                >
                  {extensionState}
                </Badge>
              </div>
              <SectionLabel>
                <span className="mt-6 block">
                  Extension popup · interactive preview
                </span>
              </SectionLabel>
              <h3 className="font-display my-4 text-2xl">
                The unreasonable effectiveness of simple systems
              </h3>
              <p className="mb-5 text-xs text-[var(--muted)]">
                worksinprogress.co
              </p>
              <div className="grid gap-3">
                <Button
                  primary
                  onClick={() =>
                    notify(
                      extensionState === "Connected"
                        ? "Extension save acknowledged (simulation). Capture pending."
                        : "Save queued locally (simulation). It will retry when connected.",
                    )
                  }
                >
                  <Bookmark size={16} />
                  Bookmark
                </Button>
                <Button
                  onClick={() =>
                    notify(
                      extensionState === "Connected"
                        ? "Saved; Kindle preparation queued (simulation)."
                        : "Kindle request queued locally (simulation).",
                    )
                  }
                >
                  <Send size={16} />
                  Send to Kindle
                </Button>
                <label className="mt-4 flex items-start gap-3 text-xs leading-6">
                  <input
                    className="mt-1"
                    type="checkbox"
                    checked={bookmarks}
                    onChange={(e) => {
                      setBookmarks(e.target.checked);
                      notify(
                        e.target.checked
                          ? "Permission flow simulated. A real extension will request browser bookmark access."
                          : "Automatic bookmark sync disabled in the preview.",
                      );
                    }}
                  />
                  Automatically save browser bookmarks
                </label>
                {bookmarks && (
                  <Badge tone="success">10,482 synced · sample</Badge>
                )}
                <label className="mt-3 grid gap-2 text-xs">
                  Try a connection state
                  <select
                    className={input}
                    value={extensionState}
                    onChange={(e) => setExtensionState(e.target.value)}
                  >
                    {[
                      "Connected",
                      "Disconnected",
                      "Invalid credentials",
                      "Server unavailable",
                      "Permission denied",
                    ].map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                </label>
              </div>
            </div>
          ) : (
            <a
              href="/connect.html"
              className="self-start rounded border border-[var(--line)] p-6 text-sm underline"
            >
              Connect browser extension and Apple Shortcut
            </a>
          )}
        </Tabs.Content>
        <Tabs.Content value="android" className="mt-7 max-w-2xl">
          <h2 className="font-display text-3xl">Attic in your share sheet</h2>
          <ol className="my-6 list-decimal space-y-4 pl-5 text-sm leading-7">
            <li>Open Attic in Chrome over HTTPS.</li>
            <li>
              Choose “Install app” or “Add to Home screen” from the browser
              menu.
            </li>
            <li>
              Share a link to Attic, then choose Bookmark or Send to Kindle.
            </li>
          </ol>
          <Button
            primary
            onClick={async () => {
              if (install) {
                await install.prompt();
                const choice = await install.userChoice;
                if (choice.outcome === "accepted") setInstall(undefined);
              } else
                notify(
                  "Use your browser menu to install. Installation requires a supported browser and HTTPS or localhost.",
                );
            }}
          >
            <Download size={16} />
            Install Attic
          </Button>
          <p className="mt-6 text-xs leading-6 text-[var(--muted)]">
            Shared links open in Attic for you to save. Connect to your server
            to preserve pages and send documents.
          </p>
        </Tabs.Content>
        <Tabs.Content value="ios" className="mt-7 max-w-2xl">
          <h2 className="font-display text-3xl">Keep Attic close on iPhone</h2>
          <ol className="my-6 list-decimal space-y-4 pl-5 text-sm leading-7">
            <li>Open Attic in Safari, tap Share, then “Add to Home Screen”.</li>
            <li>
              Use the Attic Apple Shortcut to save links from Safari. This
              connects separately to your server.
            </li>
            <li>
              Open the installed PWA to search your library and read PDFs.
            </li>
          </ol>
          <div className="rounded bg-[var(--accent-soft)] p-5 text-sm leading-7">
            iOS uses an Apple Shortcut for saving links; it does not support the
            Android PWA share target. Open the browser connection guide to
            configure your Shortcut.
          </div>
        </Tabs.Content>
      </Tabs.Root>
      <p className="mt-10 border-t border-[var(--line)] pt-5 text-xs leading-6 text-[var(--muted)]">
        Preserved pages no longer rely on the original website. Your archive
        requires a connection to the Attic server to retrieve saved content.
      </p>
    </main>
  );
}
export function ConnectPage() {
  const navigate = useNavigate();
  const { login, archive } = useArchive();
  const [busy, setBusy] = useState(false);
  const [server, setServer] = useState(window.location.origin),
    [key, setKey] = useState(""),
    [error, setError] = useState("");
  return (
    <main id="main" className="grid min-h-[calc(100dvh-30px)] md:grid-cols-2">
      <section className="flex flex-col justify-between bg-[var(--accent-soft)] p-8 md:p-16">
        <Brand />
        <div className="my-12 max-w-md">
          <h1 className="font-display text-5xl leading-tight md:text-6xl">
            Save links
            <br />
            and PDFs
          </h1>
          <p className="mt-6 text-base leading-8 text-[var(--muted)]">
            Attic saves searchable copies of links and PDFs. Send an item to
            Kindle when you want to read it there.
          </p>
        </div>
        <span className="hidden text-xs text-[var(--muted)] md:block">
          A personal archive. Entirely yours.
        </span>
      </section>
      <section className="flex items-center justify-center p-8 md:p-16">
        <form
          className="w-full max-w-sm"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!safeUrl(server) || !key.trim()) {
              setError("Enter your access key.");
              return;
            }
            setBusy(true);
            setError("");
            try {
              await login(key.trim());
              setKey("");
              if (window.location.pathname === "/connect") navigate("/");
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <h2 className="font-display text-3xl">Connect to Attic</h2>
          <p className="mb-8 mt-3 text-sm leading-6 text-[var(--muted)]">
            Enter your access key once. This browser remembers it and signs you
            in automatically until you sign out or clear its site data.
          </p>
          <label className="mb-5 grid gap-2 text-[10px] uppercase tracking-wider">
            Server address
            <input
              className={input}
              readOnly={!archive.preview}
              type="url"
              required
              value={server}
              onChange={(e) => setServer(e.target.value)}
            />
          </label>
          <label className="mb-6 grid gap-2 text-[10px] uppercase tracking-wider">
            Access key
            <input
              className={input}
              type="password"
              required
              autoComplete="off"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="Server access key"
            />
          </label>
          {error && (
            <p role="alert" className="mb-4 text-xs text-[var(--danger)]">
              {error}
            </p>
          )}
          <Button disabled={busy} primary type="submit" className="w-full">
            Connect to Attic
          </Button>
          {archive.preview && (
            <Link
              to="/"
              className="mt-6 block text-center text-xs text-[var(--muted)] underline"
            >
              Skip to UI preview
            </Link>
          )}
        </form>
      </section>
    </main>
  );
}
