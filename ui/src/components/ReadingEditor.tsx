import { useEffect, useRef, useState } from "react";
import editorStyles from "./reading-editor.css?url";
import { Button, Modal, input } from "./primitives";
import { useArchive } from "../state";

const selectable =
  "article,section,div,p,h1,h2,h3,h4,h5,h6,ul,ol,dl,li,blockquote,pre,figure,img,table,thead,tbody,tr,td,th,figcaption,hr,strong,em,a,code";
const labels: Record<string, string> = {
  article: "Article",
  section: "Section",
  div: "Block",
  p: "Paragraph",
  h1: "Heading",
  h2: "Heading",
  h3: "Heading",
  h4: "Heading",
  h5: "Heading",
  h6: "Heading",
  ul: "List",
  ol: "List",
  li: "List item",
  pre: "Code block",
  code: "Code",
  img: "Image",
  figure: "Figure",
  table: "Table",
  thead: "Table header",
  tbody: "Table body",
  tr: "Table row",
  td: "Table cell",
  th: "Header cell",
  strong: "Bold text",
  em: "Italic text",
  a: "Link",
  blockquote: "Quote",
  figcaption: "Caption",
  hr: "Divider",
};
const label = (element: Element) =>
  labels[element.tagName.toLowerCase()] || element.tagName.toLowerCase();
const shell = (html: string) =>
  `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'self'; style-src-attr 'unsafe-inline';"><link rel="stylesheet" href="${new URL(editorStyles, window.location.href).href}"></head><body>${html}</body></html>`;

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
  const hovered = useRef<Element | null>(null);
  const [ancestors, setAncestors] = useState<Element[]>([]);
  function clearHover() {
    hovered.current?.removeAttribute("data-attic-hover");
    hovered.current = null;
    frame.current?.contentDocument
      ?.querySelector("[data-attic-overlay]")
      ?.remove();
  }
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
    setAncestors([]);
    setEditable(false);
  }
  function html() {
    const body = frame.current!.contentDocument!.body.cloneNode(
      true,
    ) as HTMLElement;
    body
      .querySelectorAll("[data-attic-selected], [data-attic-hover]")
      .forEach((element) => {
        element.removeAttribute("data-attic-selected");
        element.removeAttribute("data-attic-hover");
      });
    body
      .querySelectorAll("[data-attic-overlay]")
      .forEach((element) => element.remove());
    return body.innerHTML;
  }
  function remember() {
    const snapshot = html();
    setHistory((previous) => [...previous, snapshot]);
  }
  function select(element: Element | null) {
    clearSelection();
    clearHover();
    if (!element) return;
    selected.current = element;
    element.setAttribute("data-attic-selected", "");
    setSelection(label(element));
    const path: Element[] = [];
    for (
      let node: Element | null = element;
      node && node !== frame.current?.contentDocument?.body;
      node = node.parentElement
    ) {
      if (node.matches(selectable)) path.unshift(node);
    }
    setAncestors(path);
    setText(element.textContent || "");
    setEditable(
      element.children.length === 0 && !["IMG", "HR"].includes(element.tagName),
    );
  }
  function bind() {
    const doc = frame.current?.contentDocument;
    if (!doc) return;
    doc.addEventListener("pointermove", (event) => {
      const element = (event.target as Element).closest(selectable);
      if (!element || element === doc.body) {
        clearHover();
        return;
      }
      if (element !== hovered.current) {
        clearHover();
        hovered.current = element;
        element.setAttribute("data-attic-hover", "");
      }
      let overlay = doc.querySelector<HTMLElement>("[data-attic-overlay]");
      if (!overlay) {
        overlay = doc.createElement("div");
        overlay.setAttribute("data-attic-overlay", "");
        doc.body.append(overlay);
      }
      const rect = element.getBoundingClientRect();
      overlay.textContent = `${label(element)} · ${Math.round(rect.width)} × ${Math.round(rect.height)}`;
      overlay.style.left = `${Math.max(8, Math.min(event.clientX + 12, doc.documentElement.clientWidth - overlay.offsetWidth - 8))}px`;
      overlay.style.top = `${Math.max(8, Math.min(event.clientY + 16, doc.documentElement.clientHeight - overlay.offsetHeight - 8))}px`;
    });
    doc.addEventListener("pointerleave", clearHover);
    doc.addEventListener("scroll", clearHover, true);
    doc.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        clearHover();
        clearSelection();
      }
    });
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
                  clearHover();
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
                  clearHover();
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
            {!!ancestors.length && (
              <nav
                aria-label="Selected element path"
                className="flex flex-wrap items-center gap-1 text-xs"
              >
                {ancestors.map((element, index) => (
                  <span key={index} className="inline-flex items-center gap-1">
                    {index > 0 && <span aria-hidden="true">›</span>}
                    <button
                      className="min-h-9 rounded px-2 hover:bg-[var(--accent-soft)]"
                      aria-current={
                        element === selected.current ? "true" : undefined
                      }
                      disabled={busy}
                      onClick={() => select(element)}
                    >
                      {label(element)}
                    </button>
                  </span>
                ))}
              </nav>
            )}
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
