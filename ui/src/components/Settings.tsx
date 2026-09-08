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
  Check,
  ExternalLink,
  Globe,
} from "lucide-react";
import * as Tabs from "@radix-ui/react-tabs";
import { Badge, Brand, Button, Modal, SectionLabel, input } from "./primitives";
import { makeItem, safeUrl, seed, validItems, type Item } from "../model";
import { usePreview } from "../state";
export function SettingsPage() {
  const { items, setItems, openUpload, notify } = usePreview();
  const [busy, setBusy] = useState(false),
    [result, setResult] = useState(""),
    [reset, setReset] = useState(false),
    [empty, setEmpty] = useState(false);
  const importer = useRef<HTMLInputElement>(null),
    restore = useRef<HTMLInputElement>(null);
  async function importFile(file?: File, restoring = false) {
    if (!file) return;
    setBusy(true);
    setResult("");
    try {
      if (file.size > 10 * 1024 * 1024)
        throw new Error("Choose a file smaller than 10 MB for this preview.");
      const text = await file.text();
      let incoming: Item[];
      let skipped = 0;
      if (restoring) {
        const data = JSON.parse(text);
        if (data.format !== "attic-ui-preview-v1" || !validItems(data.items))
          throw new Error(
            "Choose an Attic UI preview JSON export. Server archive restore will be added during integration.",
          );
        incoming = data.items;
      } else {
        const doc = new DOMParser().parseFromString(text, "text/html");
        const links = [...doc.querySelectorAll("a[href]")];
        incoming = links.flatMap((a) => {
          const url = safeUrl(a.getAttribute("href") || "");
          if (!url) {
            skipped++;
            return [];
          }
          return [
            {
              ...makeItem(url),
              title: a.textContent?.trim() || new URL(url).hostname,
              folder: "Imported browser bookmarks",
            },
          ];
        });
        if (!incoming.length)
          throw new Error(
            "No valid web bookmarks were found in this HTML file.",
          );
      }
      const merged = [...items];
      let added = 0,
        duplicates = 0;
      for (const next of incoming) {
        if (
          merged.some(
            (i) => i.id === next.id || (next.url && i.url === next.url),
          )
        ) {
          duplicates++;
          continue;
        }
        merged.push({ ...next, delivery: "Not requested" });
        added++;
      }
      setItems(merged);
      setResult(
        `${added} added · ${duplicates} merged · ${skipped} skipped. Existing notes and tags kept. No sites visited or emails sent.`,
      );
      notify(
        restoring
          ? "Preview records restored."
          : "Browser bookmarks imported into the preview.",
      );
    } catch (e) {
      setResult(e instanceof Error ? e.message : "Unable to read this file.");
    } finally {
      setBusy(false);
      if (importer.current) importer.current.value = "";
      if (restore.current) restore.current.value = "";
    }
  }
  function exportRecords() {
    const blob = new Blob(
      [JSON.stringify({ format: "attic-ui-preview-v1", items }, null, 2)],
      { type: "application/json" },
    );
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "attic-ui-preview.json";
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    setResult(
      "Preview records exported. Local PDF files are not included; this is not a full archive backup.",
    );
  }
  return (
    <main id="main" className="mx-auto max-w-[1600px] px-5 pb-28 pt-8 sm:px-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-4xl">Settings</h1>
          <p className="mt-3 text-xs leading-6 text-[var(--muted)]">
            Manage storage, imports, backups, and required connections.
          </p>
        </div>
        <Link
          to="/"
          className="inline-flex min-h-11 items-center gap-2 text-xs"
        >
          <ArrowLeft size={14} />
          Open library
        </Link>
      </div>
      <section className="mt-6 grid gap-7 border border-[var(--line)] bg-[var(--paper)] p-6 md:grid-cols-[220px_1fr_230px]">
        <div>
          <SectionLabel>Storage used · sample</SectionLabel>
          <p className="font-display mt-3 text-4xl">38.4 GB</p>
          <p className="mt-2 text-xs text-[var(--muted)]">
            {items.length} items in this preview
          </p>
        </div>
        <div className="flex flex-col justify-center">
          <div
            role="img"
            aria-label="Sample storage: saved pages 25.6 GB, PDFs 11.9 GB, records and indexes 0.9 GB"
            className="flex h-3 gap-0.5 overflow-hidden rounded-sm"
          >
            <span className="w-2/3 bg-[var(--accent)]" />
            <span className="w-[31%] bg-[var(--warning)]" />
            <span className="flex-1 bg-[var(--line)]" />
          </div>
          <div className="mt-4 flex flex-wrap gap-4 text-[10px] text-[var(--muted)]">
            <span>Saved pages · 25.6 GB</span>
            <span>PDFs · 11.9 GB</span>
            <span>Records & indexes · 0.9 GB</span>
          </div>
          <p className="mt-4 text-[11px] leading-5 text-[var(--muted)]">
            Nothing is pruned automatically. Remove saved content explicitly
            from its item page.
          </p>
        </div>
        <div className="border-t border-[var(--line)] pt-5 md:border-l md:border-t-0 md:pl-6 md:pt-0">
          <SectionLabel>Connections · sample</SectionLabel>
          <div className="mt-4 flex items-center justify-between text-xs">
            <span>Kindle delivery</span>
            <Badge tone="success">Configured</Badge>
          </div>
          <div className="mt-3 flex items-center justify-between text-xs">
            <span>Search</span>
            <Badge tone="success">Available</Badge>
          </div>
          <p className="mt-3 text-[10px] text-[var(--muted)]">
            Server managed · not connected
          </p>
        </div>
      </section>
      <div className="mb-5 mt-9 flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-display text-2xl">Import and backup</h2>
        <span className="text-[11px] text-[var(--muted)]">
          Preview actions work with local records.
        </span>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          {
            title: "Upload PDF",
            body: "Keep the original file unchanged. Open it in the reader on this device.",
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
            body: "Try a preview records export. Full backups with preserved files await integration.",
            label: "Export preview records",
            icon: Archive,
            action: exportRecords,
          },
          {
            title: "Restore archive",
            body: "Merge a preview JSON export safely. Local PDF files must remain on this device.",
            label: "Choose preview export",
            icon: RotateCcw,
            action: () => restore.current?.click(),
          },
        ].map(({ title, body, label, icon: Icon, action }, index) => (
          <section
            key={title}
            className="flex min-h-[230px] flex-col items-start border border-[var(--line)] bg-[var(--paper)] p-5"
          >
            <Icon size={20} className="text-[var(--muted)]" />
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
        accept=".html,.htm,text/html"
        className="hidden"
        onChange={(e) => void importFile(e.target.files?.[0])}
      />
      <input
        ref={restore}
        aria-label="Restore preview records"
        type="file"
        accept=".json,application/json"
        className="hidden"
        onChange={(e) => void importFile(e.target.files?.[0], true)}
      />
      {(busy || result) && (
        <div
          role="status"
          className="mt-5 rounded border border-[var(--line)] bg-[var(--paper)] p-5 text-sm leading-6"
        >
          {busy ? "Checking records…" : result}
        </div>
      )}
      <section className="mt-8 flex flex-wrap items-center justify-between gap-5 border-y border-[var(--line)] py-6">
        <div>
          <h2 className="font-display text-2xl">Save from anywhere</h2>
          <p className="mt-2 text-xs text-[var(--muted)]">
            Your browser, your phone, your everyday tools.
          </p>
        </div>
        <Link
          to="/setup"
          className="inline-flex min-h-11 items-center gap-2 rounded border border-[var(--line)] bg-[var(--paper)] px-4 text-xs"
        >
          Set up sharing
          <ExternalLink size={14} />
        </Link>
      </section>
      <section className="mt-8">
        <SectionLabel>Try the preview</SectionLabel>
        <p className="mb-4 mt-2 text-xs leading-6 text-[var(--muted)]">
          Explore first use or reset sample records. These controls affect only
          this UI preview.
        </p>
        <div className="flex flex-wrap gap-3">
          <Link
            to="/connect"
            className="inline-flex min-h-11 items-center rounded border border-[var(--line)] bg-[var(--paper)] px-4 text-xs"
          >
            Preview connection screen
          </Link>
          <Button onClick={() => setEmpty(true)}>Try empty library</Button>
          <Button onClick={() => setReset(true)}>Reset sample data</Button>
        </div>
      </section>
      <Modal
        open={reset || empty}
        onOpenChange={() => {
          setReset(false);
          setEmpty(false);
        }}
        title={empty ? "Try an empty library?" : "Reset preview records?"}
        description="This replaces the current preview records. Export them first if you want to keep your changes. Uploaded PDF files remain on this device."
      >
        <div className="flex justify-end gap-3">
          <Button
            onClick={() => {
              setReset(false);
              setEmpty(false);
            }}
          >
            Cancel
          </Button>
          <Button
            primary
            onClick={() => {
              setItems(empty ? [] : structuredClone(seed));
              setReset(false);
              setEmpty(false);
              notify("Preview records updated.");
            }}
          >
            Continue
          </Button>
        </div>
      </Modal>
    </main>
  );
}
interface InstallEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: string }>;
}
export function SetupPage() {
  const { notify } = usePreview();
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
              <li>Load the existing Attic extension in your browser.</li>
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
            Install Attic preview
          </Button>
          <p className="mt-6 text-xs leading-6 text-[var(--muted)]">
            The installed preview accepts shared links into local sample data.
            Server integration will be required to preserve pages or send
            documents.
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
            Android PWA share target. Shortcut connection will be included
            during integration.
          </div>
        </Tabs.Content>
      </Tabs.Root>
      <p className="mt-10 border-t border-[var(--line)] pt-5 text-xs leading-6 text-[var(--muted)]">
        Preserved pages no longer rely on the original website. Your real
        archive will still need a connection to the Attic server. This local
        preview is not a fully offline archive.
      </p>
    </main>
  );
}
export function ConnectPage() {
  const navigate = useNavigate();
  const [server, setServer] = useState("https://attic.home"),
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
          onSubmit={(e) => {
            e.preventDefault();
            if (!safeUrl(server) || !key.trim()) {
              setError("Enter a valid server URL and any sample access key.");
              return;
            }
            navigate("/");
          }}
        >
          <h2 className="font-display text-3xl">Connect to Attic</h2>
          <p className="mb-8 mt-3 text-sm leading-6 text-[var(--muted)]">
            Preview this flow with a sample key. No credentials are sent or
            stored.
          </p>
          <label className="mb-5 grid gap-2 text-[10px] uppercase tracking-wider">
            Server address
            <input
              className={input}
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
              placeholder="Use any sample key"
            />
          </label>
          {error && (
            <p role="alert" className="mb-4 text-xs text-[var(--danger)]">
              {error}
            </p>
          )}
          <Button primary type="submit" className="w-full">
            Connect to Attic
          </Button>
          <Link
            to="/"
            className="mt-6 block text-center text-xs text-[var(--muted)] underline"
          >
            Skip to UI preview
          </Link>
        </form>
      </section>
    </main>
  );
}
