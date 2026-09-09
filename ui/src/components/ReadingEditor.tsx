import { useEffect, useRef, useState } from "react";
import { Button, Modal, input } from "./primitives";
import { useArchive } from "../state";

const selectable =
  "p,h1,h2,h3,h4,h5,h6,li,blockquote,pre,figure,img,table,td,th,figcaption,hr,strong,em,a,code";
const shell = (html: string) =>
  `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src http: https: data:; style-src 'unsafe-inline';"><style>body{margin:24px;font:18px/1.65 Georgia,serif;color:#24221f;overflow-wrap:anywhere}img,table{max-width:100%}table{border-collapse:collapse}td,th{border:1px solid #aaa;padding:8px}pre{white-space:pre-wrap;font-size:14px}a{color:inherit}[data-attic-selected]{outline:2px solid #a56332;outline-offset:3px;background:#a5633214}p,h1,h2,h3,li,img,table,pre{cursor:pointer}</style></head><body>${html}</body></html>`;

export function ReadingEditor({
  itemID,
  onClose,
  onSaved,
}: {
  itemID: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { archive } = useArchive();
  const frame = useRef<HTMLIFrameElement>(null);
  const selected = useRef<Element | null>(null);
  const [source, setSource] = useState("");
  const [revision, setRevision] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const [selection, setSelection] = useState("");
  const [text, setText] = useState("");
  const [editable, setEditable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    archive
      .editor(itemID, controller.signal)
      .then((data) => {
        setRevision(data.revision);
        setSource(shell(data.html));
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(error.message);
      });
    return () => controller.abort();
  }, [archive, itemID]);
  function clearSelection() {
    selected.current?.removeAttribute("data-attic-selected");
    selected.current = null;
    setSelection("");
    setEditable(false);
  }
  function html() {
    const body = frame.current!.contentDocument!.body.cloneNode(
      true,
    ) as HTMLElement;
    body
      .querySelectorAll("[data-attic-selected]")
      .forEach((element) => element.removeAttribute("data-attic-selected"));
    return body.innerHTML;
  }
  function remember() {
    const snapshot = html();
    setHistory((previous) => [...previous, snapshot]);
  }
  function select(element: Element | null) {
    clearSelection();
    if (!element) return;
    selected.current = element;
    element.setAttribute("data-attic-selected", "");
    setSelection(element.tagName.toLowerCase());
    setText(element.textContent || "");
    setEditable(
      element.children.length === 0 && !["IMG", "HR"].includes(element.tagName),
    );
  }
  function bind() {
    const doc = frame.current?.contentDocument;
    if (!doc) return;
    doc.addEventListener("click", (event) => {
      event.preventDefault();
      select((event.target as Element).closest(selectable));
    });
    doc.addEventListener("submit", (event) => event.preventDefault());
  }
  return (
    <Modal
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
      title="Edit reading version"
      wide
    >
      <div className="space-y-4">
        {error && (
          <p role="alert" className="text-sm text-[var(--danger)]">
            {error}
          </p>
        )}
        {!source && !error && <p role="status">Opening reading version…</p>}
        {source && (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                disabled={!selection || busy}
                onClick={() => {
                  remember();
                  selected.current?.remove();
                  clearSelection();
                }}
              >
                Hide selected
              </Button>
              <Button
                disabled={
                  busy || !selected.current?.parentElement?.closest(selectable)
                }
                onClick={() =>
                  select(
                    selected.current?.parentElement?.closest(selectable) ||
                      null,
                  )
                }
              >
                Select parent
              </Button>
              <Button
                disabled={!history.length || busy}
                onClick={() => {
                  const previous = history.at(-1)!;
                  clearSelection();
                  frame.current!.contentDocument!.body.innerHTML = previous;
                  setHistory(history.slice(0, -1));
                }}
              >
                Undo
              </Button>
              {selection && (
                <span className="text-xs text-[var(--muted)]">
                  Selected: {selection}
                </span>
              )}
            </div>
            <iframe
              ref={frame}
              title="Reading editor"
              sandbox="allow-same-origin"
              srcDoc={source}
              onLoad={bind}
              className={`h-[45dvh] w-full rounded border border-[var(--line)] bg-white ${busy ? "pointer-events-none" : ""}`}
            />
            {editable && (
              <div className="space-y-2">
                <label className="grid gap-2 text-xs">
                  Selected text
                  <textarea
                    className={input}
                    rows={3}
                    value={text}
                    disabled={busy}
                    onChange={(event) => setText(event.target.value)}
                  />
                </label>
                <Button
                  disabled={busy || text === selected.current?.textContent}
                  onClick={() => {
                    remember();
                    if (selected.current) selected.current.textContent = text;
                    clearSelection();
                  }}
                >
                  Apply text
                </Button>
              </div>
            )}
          </>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <Button disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button
            primary
            disabled={!source || !history.length || busy}
            onClick={async () => {
              setBusy(true);
              setError("");
              try {
                await archive.saveReading(itemID, html(), revision);
                onSaved();
              } catch (error) {
                setError((error as Error).message);
              } finally {
                setBusy(false);
              }
            }}
          >
            {busy ? "Saving…" : "Save and generate PDF"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
