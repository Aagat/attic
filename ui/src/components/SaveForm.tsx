import { randomId } from "../id";
import { useRef, useState } from "react";
import { Bookmark, Send, Link as LinkIcon, Settings } from "lucide-react";
import { Link } from "react-router-dom";
import { Button, input } from "./primitives";
import { safeUrl } from "../model";
import { useArchive } from "../state";
export function SaveForm({
  initial = "",
  onSaved,
  sidebar = false,
}: {
  initial?: string;
  onSaved?: () => void;
  sidebar?: boolean;
}) {
  const [url, setUrl] = useState(initial),
    [error, setError] = useState("");
  const { save, archive } = useArchive();
  const [busy, setBusy] = useState(false);
  const attempt = useRef({ url: "", send: false, key: "" });
  async function submit(send: boolean) {
    const valid = safeUrl(url.trim());
    if (!valid) {
      setError("Enter a complete http:// or https:// URL.");
      return;
    }
    if (busy) return;
    if (attempt.current.url !== valid || attempt.current.send !== send)
      attempt.current = { url: valid, send, key: randomId() };
    setBusy(true);
    try {
      await save(valid, send, attempt.current.key);
      setUrl("");
      setError("");
      onSaved?.();
      attempt.current.url = "";
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        submit(false);
      }}
      className={`flex flex-col gap-4 ${sidebar ? "min-h-[550px] rounded border border-[var(--line)] bg-[var(--paper)] p-5" : ""}`}
    >
      {sidebar && <h2 className="font-display text-[28px]">Save a link</h2>}
      <p className="text-xs leading-5 text-[var(--muted)]">
        Saving happens immediately. Capture and indexing continue in the
        background.
      </p>
      <label className="relative">
        <span className="sr-only">URL to save</span>
        <LinkIcon className="absolute left-3 top-3.5 size-4 text-[var(--muted)]" />
        <input
          className={`${input} pl-9`}
          type="url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="Paste a URL…"
          aria-invalid={!!error}
          aria-describedby={error ? "url-error" : undefined}
        />
      </label>
      {error && (
        <p id="url-error" role="alert" className="text-xs text-[var(--danger)]">
          {error}
        </p>
      )}
      <Button
        disabled={busy}
        primary
        type="submit"
        className="justify-start py-3"
      >
        <Bookmark size={18} />
        <span className="text-left">
          Bookmark
          <span className="mt-1 block text-[11px] font-normal">
            Save and archive in background
          </span>
        </span>
      </Button>
      <Button
        disabled={busy}
        type="button"
        onClick={() => submit(true)}
        className="justify-start py-3"
      >
        <Send size={18} />
        <span className="text-left">
          Send to Kindle
          <span className="mt-1 block text-[11px] font-normal text-[var(--muted)]">
            Save, prepare, then deliver
          </span>
        </span>
      </Button>
      {archive.preview && (
        <p className="text-[11px] leading-5 text-[var(--muted)]">
          Preview: saves stay on this device. Processing and delivery are
          simulated.
        </p>
      )}
      {sidebar && (
        <Link
          to="/settings"
          className="mt-auto flex min-h-11 items-center gap-2 border-t border-[var(--line)] pt-4 text-xs"
        >
          <Settings size={15} />
          Settings
        </Link>
      )}
    </form>
  );
}
