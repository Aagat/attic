import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import * as Tabs from "@radix-ui/react-tabs";
import {
  ArrowLeft,
  ArrowRight,
  Download,
  Share2,
  Send,
  Info,
  Pencil,
  ExternalLink,
  RefreshCw,
  CheckCircle2,
  Trash2,
  Plus,
  Minus,
  PanelRightClose,
  PanelRightOpen,
} from "lucide-react";
import type { PDFDocumentProxy } from "pdfjs-dist";
import { Badge, Brand, Button, Modal, SectionLabel, input } from "./primitives";
import { BrowserRecovery } from "./BrowserRecovery";
import { dateLabel, safeUrl, type Item } from "../model";
import { useArchive, useArchiveQuery } from "../state";

function SavedCapture({
  item,
  index,
  reading,
}: {
  item: Item;
  index: number;
  reading: boolean;
}) {
  const { archive } = useArchive();
  const { data: url, error } = useArchiveQuery(
    `capture:${item.id}:${item.versions[index]?.id}:${reading}`,
    (signal) => archive.captureURL(item, index, reading, signal),
  );
  if (error)
    return (
      <p role="alert" className="p-8 text-sm leading-7">
        {error}
      </p>
    );
  return url ? (
    <iframe
      title={reading ? "Saved reading version" : "Saved original layout"}
      src={url}
      sandbox=""
      className="w-full min-h-[80dvh] border-0"
    />
  ) : (
    <p role="status" className="p-8">
      Opening saved copy…
    </p>
  );
}
const languages = [
  ["en", "English"],
  ["es", "Spanish"],
  ["fr", "French"],
  ["de", "German"],
  ["it", "Italian"],
  ["pt", "Portuguese"],
  ["nl", "Dutch"],
  ["ja", "Japanese"],
  ["ko", "Korean"],
  ["zh", "Chinese"],
];
function TranslatedReading({ item }: { item: Item }) {
  const { archive } = useArchive();
  const { data: url, error } = useArchiveQuery(
    `reading:${item.id}:${item.jobId}`,
    (signal) => archive.readingURL(item, signal),
  );
  if (error)
    return (
      <p role="alert" className="p-8 text-sm">
        {error}
      </p>
    );
  return url ? (
    <iframe
      title="Translated reading version"
      src={url}
      sandbox=""
      className="w-full min-h-[80dvh] border-0"
    />
  ) : (
    <p role="status" className="p-8">
      Opening translation…
    </p>
  );
}
const prose = [
  "Complex systems are usually built one decision at a time. Each addition feels locally reasonable: another setting, another status, another escape hatch. Yet the systems we return to are often the ones that ask less of us.",
  "Simplicity is not the absence of capability. It is the result of arranging capability so that the common path remains obvious. The archive that survives is the archive whose owner can trust it without tending it every day.",
  "A useful principle follows: preserve the record first, then improve it. Background work may enrich what was saved, but should never determine whether it exists.",
];
export function Reader() {
  const { id } = useParams();
  const { archive } = useArchive();
  const { data: item, error } = useArchiveQuery("item:" + id, (signal) =>
    archive.get(id!, signal),
  );
  if (!item && !error)
    return (
      <main id="main" className="p-10" role="status">
        Loading item…
      </main>
    );
  return item ? (
    <ReaderItem key={item.id} item={item} />
  ) : (
    <main id="main" className="p-10 text-center">
      <h1 className="font-display text-3xl">{error}</h1>
      <Link to="/" className="mt-5 inline-block underline">
        Return to library
      </Link>
    </main>
  );
}
function ReaderItem({ item }: { item: Item }) {
  const {
    archive,
    refresh,
    notify,
    send,
    recapture,
    generate,
    remove: removeItems,
  } = useArchive();
  const navigate = useNavigate();
  const [format, setFormat] = useState(item.kind === "PDF" ? "pdf" : "reading");
  const hasPdf = archive.preview ? !!item.fileId : !!item.hasPdf;
  const showPdf = item.kind === "PDF" || (format === "pdf" && hasPdf);
  const [recoveryOpen, setRecoveryOpen] = useState(false);
  const [language, setLanguage] = useState(item.outputLanguage || "en");
  const [translating, setTranslating] = useState(false);
  const [translationError, setTranslationError] = useState("");
  async function translate(target: string) {
    setTranslating(true);
    setTranslationError("");
    try {
      await archive.translate(item.id, target);
      setFormat("reading");
      refresh();
    } catch (error) {
      setTranslationError((error as Error).message);
    } finally {
      setTranslating(false);
    }
  }
  const [informationOpen, setInformationOpen] = useState(true);
  const [details, setDetails] = useState(false),
    [remove, setRemove] = useState(false),
    [resend, setResend] = useState(false),
    [title, setTitle] = useState(item.title),
    [notes, setNotes] = useState(item.notes),
    [tags, setTags] = useState(item.tags.join(", ")),
    [version, setVersion] = useState(0);
  const [page, setPage] = useState(() => {
      try {
        return Math.max(
          1,
          Number(localStorage.getItem(`attic-page-${item.id}`)) || 1,
        );
      } catch {
        return 1;
      }
    }),
    [zoom, setZoom] = useState(100),
    [file, setFile] = useState<File>(),
    [fileError, setFileError] = useState(""),
    [pdf, setPdf] = useState<PDFDocumentProxy>(),
    [pdfError, setPdfError] = useState(""),
    [pdfLoading, setPdfLoading] = useState(false);
  const canvas = useRef<HTMLCanvasElement>(null),
    isPdf = item.kind === "PDF",
    total = pdf?.numPages || item.pages || 1;
  const current = Math.min(page, total);
  useEffect(() => {
    setFile(undefined);
    setFileError("");
    setPdf(undefined);
    setPdfError("");
    if (archive.preview ? !item.fileId : !item.hasPdf) return;
    let active = true;
    archive
      .pdf(item)
      .then((f) => {
        if (active) {
          setFile(f);
          if (!f)
            setFileError(
              "The local PDF is missing. Upload it again to read it.",
            );
        }
      })
      .catch(() => {
        if (active) setFileError("This device could not open the stored PDF.");
      });
    return () => {
      active = false;
    };
  }, [item.fileId, item.hasPdf, item.jobId, archive]);
  useEffect(() => {
    if (!file) return;
    let active = true;
    let document: PDFDocumentProxy | undefined;
    setPdfLoading(true);
    async function load() {
      try {
        const lib = await import("pdfjs-dist");
        lib.GlobalWorkerOptions.workerSrc = new URL(
          "pdfjs-dist/build/pdf.worker.min.mjs",
          import.meta.url,
        ).href;
        const result = await lib.getDocument({
          data: await file!.arrayBuffer(),
        }).promise;
        document = result;
        if (active) {
          setPdf(result);
          setPdfLoading(false);
        } else void result.loadingTask.destroy();
      } catch {
        if (active) {
          setPdfError(
            "This PDF could not be rendered. It may be encrypted or damaged. Your original file is still available to download.",
          );
          setPdfLoading(false);
        }
      }
    }
    void load();
    return () => {
      active = false;
      if (document) void document.loadingTask.destroy();
    };
  }, [file]);
  useEffect(() => {
    if (!showPdf || !pdf || !canvas.current) return;
    let active = true;
    let task: { cancel: () => void; promise: Promise<void> } | undefined;
    void pdf
      .getPage(current)
      .then((p) => {
        if (!active || !canvas.current) return;
        const view = p.getViewport({ scale: 1.4 });
        const el = canvas.current;
        el.width = view.width;
        el.height = view.height;
        task = p.render({ canvas: el, viewport: view });
        return task.promise;
      })
      .catch((e) => {
        if (active && e?.name !== "RenderingCancelledException")
          setPdfError(
            "This page could not be rendered. Try another page or download the original.",
          );
      });
    return () => {
      active = false;
      task?.cancel();
    };
  }, [pdf, current, showPdf]);
  useEffect(() => {
    try {
      localStorage.setItem(`attic-page-${item.id}`, String(current));
    } catch {
      /* Position is optional when device storage is unavailable. */
    }
  }, [current, item.id]);
  const selected = item.versions[version];
  const safe = safeUrl(item.url);
  function requestSend() {
    if (item.delivery === "Outcome uncertain") setResend(true);
    else send(item.id);
  }
  function download() {
    if (!file) {
      notify(
        "A PDF is not available yet. Try again after preparation finishes.",
      );
      return;
    }
    const url = URL.createObjectURL(file),
      a = document.createElement("a");
    a.href = url;
    a.download = file.name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  async function share() {
    if (!file) {
      notify("A PDF is not available for sharing yet.");
      return;
    }
    if (navigator.canShare?.({ files: [file] })) {
      try {
        await navigator.share({ files: [file], title: item.title });
      } catch (e) {
        if ((e as Error).name !== "AbortError")
          notify("Sharing was unavailable. You can download the file instead.");
      }
    } else {
      download();
      notify(
        "File sharing is unavailable in this browser. Downloaded the original instead.",
      );
    }
  }
  return (
    <main id="main" className="pb-24 lg:pb-0">
      <header className="sticky top-0 z-20 flex flex-wrap items-center justify-between gap-3 border-b border-[var(--line)] bg-[var(--paper)] px-5 py-3 sm:px-8">
        <Brand />
        <div className="flex flex-wrap gap-2">
          {isPdf ? (
            <>
              <Button onClick={download}>
                <Download size={15} />
                Download
              </Button>
              <Button onClick={() => void share()}>
                <Share2 size={15} />
                Share
              </Button>
            </>
          ) : (
            safe && (
              <a
                href={safe}
                target="_blank"
                rel="noreferrer"
                className="inline-flex min-h-11 items-center gap-2 rounded border border-[var(--line)] px-3 text-xs"
              >
                <ExternalLink size={14} />
                Original site
              </a>
            )
          )}
          {!isPdf && (
            <Button onClick={() => setRecoveryOpen(true)}>
              Continue capture
            </Button>
          )}
          <Button
            onClick={() => {
              setTitle(item.title);
              setNotes(item.notes);
              setTags(item.tags.join(", "));
              setDetails(true);
            }}
          >
            <Info size={15} />
            Item details
          </Button>
          <Button
            primary
            disabled={item.delivery === "Preparing document"}
            onClick={requestSend}
          >
            <Send size={15} />
            {item.delivery === "Preparing document"
              ? "Preparing…"
              : "Send to Kindle"}
          </Button>
        </div>
      </header>
      <Tabs.Root value={format} onValueChange={setFormat}>
        <section className="flex flex-wrap items-center justify-between gap-5 border-b border-[var(--line)] bg-[var(--paper)] px-5 py-5 sm:px-8">
          <div className="min-w-0 max-w-3xl">
            <div className="flex items-start gap-3">
              <h1 className="font-display text-2xl leading-tight">
                {item.title}
              </h1>
              <Button
                aria-label="Edit title"
                onClick={() => {
                  setTitle(item.title);
                  setNotes(item.notes);
                  setTags(item.tags.join(", "));
                  setDetails(true);
                }}
              >
                <Pencil size={15} />
              </Button>
            </div>
            <p className="mt-2 text-[11px] leading-5 text-[var(--muted)]">
              {item.author && `${item.author} · `}
              {item.source} · Saved {dateLabel(item.saved)}
              {isPdf && ` · ${total} pages`}
            </p>
          </div>
        </section>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--line)] px-5 py-3 sm:px-8">
          {!isPdf && (
            <Tabs.List
              aria-label="Reading format"
              className="flex flex-wrap gap-2"
            >
              {[
                ["reading", "Reading version"],
                ["original", "Original layout"],
                ...(hasPdf ? [["pdf", "PDF"]] : []),
              ].map(([value, label]) => (
                <Tabs.Trigger
                  key={value}
                  value={value}
                  className="min-h-11 rounded border border-transparent px-3 text-xs data-[state=active]:border-[var(--line)] data-[state=active]:bg-[var(--paper)]"
                >
                  {label}
                </Tabs.Trigger>
              ))}
            </Tabs.List>
          )}
          {showPdf ? (
            <>
              <div className="flex items-center gap-2">
                <Button
                  aria-label="Previous PDF page"
                  disabled={current === 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                >
                  <ArrowLeft size={16} />
                </Button>
                <label className="flex items-center gap-2 text-xs">
                  <span className="sr-only">PDF page</span>
                  <input
                    aria-label="PDF page"
                    type="number"
                    min={1}
                    max={total}
                    className={`${input} w-16! py-2!`}
                    value={current}
                    onChange={(e) =>
                      setPage(
                        Math.min(
                          total,
                          Math.max(1, Number(e.target.value) || 1),
                        ),
                      )
                    }
                  />
                  of {total}
                </label>
                <Button
                  aria-label="Next PDF page"
                  disabled={current >= total}
                  onClick={() => setPage((p) => Math.min(total, p + 1))}
                >
                  <ArrowRight size={16} />
                </Button>
              </div>
              <span className="hidden text-[11px] text-[var(--muted)] sm:block">
                Position saved automatically
              </span>
              <div className="flex items-center gap-2">
                <Button
                  aria-label="Zoom out"
                  disabled={zoom === 75}
                  onClick={() => setZoom((z) => z - 25)}
                >
                  <Minus size={14} />
                </Button>
                <span className="w-10 text-center text-xs">{zoom}%</span>
                <Button
                  aria-label="Zoom in"
                  disabled={zoom === 150}
                  onClick={() => setZoom((z) => z + 25)}
                >
                  <Plus size={14} />
                </Button>
              </div>
            </>
          ) : (
            <>
              <span className="text-[11px] text-[var(--muted)]">
                {selected
                  ? `Captured ${dateLabel(selected.date)}`
                  : "No capture available yet"}
              </span>
            </>
          )}
          {!informationOpen && (
            <Button
              aria-label="Show item information"
              aria-expanded={false}
              aria-controls="item-information"
              onClick={() => setInformationOpen(true)}
            >
              <PanelRightOpen size={18} />
            </Button>
          )}
        </div>
        <div
          className={`grid gap-5 p-4 sm:p-6 ${informationOpen ? "lg:grid-cols-[minmax(0,1fr)_285px]" : "grid-cols-1"}`}
        >
          <div className="min-w-0 bg-[var(--paper)]">
            {showPdf ? (
              <Tabs.Content value="pdf">
                <div className="min-h-[65dvh] overflow-auto bg-[var(--line)]/30 p-4 sm:p-8">
                  <div
                    className={`mx-auto min-h-[700px] bg-[var(--paper)] p-6 shadow-sm sm:p-12 ${zoom === 75 ? "max-w-[510px]" : zoom === 100 ? "max-w-[680px]" : zoom === 125 ? "w-[850px]" : "w-[1020px]"}`}
                  >
                    {!archive.preview && !item.hasPdf ? (
                      <p role="alert" className="text-sm">
                        The original PDF is unavailable. Your saved record is
                        still here.
                      </p>
                    ) : item.fileId || item.hasPdf ? (
                      <>
                        {pdfLoading && (
                          <p role="status" className="text-sm">
                            Opening PDF…
                          </p>
                        )}
                        {(fileError || pdfError) && (
                          <p
                            role="alert"
                            className="text-sm text-[var(--danger)]"
                          >
                            {fileError || pdfError}
                          </p>
                        )}
                        <canvas
                          ref={canvas}
                          className="h-auto w-full"
                          aria-label={`PDF page ${current} of ${total}`}
                        />
                        <p className="mt-4 text-xs text-[var(--muted)]">
                          For selectable text and assistive reading, download
                          the original PDF.
                        </p>
                      </>
                    ) : (
                      <>
                        <SectionLabel>
                          Sample document · page {current}
                        </SectionLabel>
                        <h2 className="font-display mb-8 mt-7 text-4xl">
                          {current === 1
                            ? "Encoding and Evolution"
                            : `Field notes · ${current}`}
                        </h2>
                        <div className="font-display space-y-6 text-lg leading-8">
                          {prose.map((t) => (
                            <p key={t}>{t}</p>
                          ))}
                        </div>
                        <div className="mt-10 border-t border-[var(--line)] pt-6 text-xs leading-6 text-[var(--muted)]">
                          This is a layout sample, not the original publication.
                          Upload your own PDF to try real page rendering,
                          download, and sharing.
                        </div>
                        <p className="mt-14 text-center font-display">
                          {current}
                        </p>
                      </>
                    )}
                  </div>
                </div>
              </Tabs.Content>
            ) : !archive.preview ? (
              <>
                {(["reading", "original"] as const).map((view) => (
                  <Tabs.Content key={view} value={view}>
                    {view === "reading" && item.outputLanguage ? (
                      item.readingAvailable ? (
                        <TranslatedReading item={item} />
                      ) : (
                        <p
                          role={
                            item.pdfStatus === "failed" ? "alert" : "status"
                          }
                          className="p-8 text-sm leading-7"
                        >
                          {item.pdfStatus === "failed"
                            ? "Translation failed. Try translating again or switch to Original."
                            : "Preparing your translation. The original saved copy is still available in Original layout."}
                        </p>
                      )
                    ) : item.versions[version] ? (
                      <SavedCapture
                        item={item}
                        index={version}
                        reading={view === "reading"}
                      />
                    ) : (
                      <div className="p-8 text-sm leading-7">
                        Your link is saved.{" "}
                        {item.capture === "Needs browser capture"
                          ? "This site blocked server capture. Open it in Chromium, complete any verification, then save the page with the Attic extension."
                          : item.capture === "Capture failed"
                            ? "Capture failed. You can request a fresh capture."
                            : item.capture === "Not captured"
                              ? "This item has no saved page yet. Request a capture to preserve it."
                              : item.capture === "Capture status unavailable"
                                ? "Capture status is unavailable. You can request a fresh capture."
                                : "The saved page will appear when capture finishes."}
                      </div>
                    )}
                  </Tabs.Content>
                ))}
              </>
            ) : (
              <>
                <Tabs.Content
                  value="reading"
                  className="mx-auto min-h-[68dvh] max-w-[930px] p-6 sm:p-12"
                >
                  <SectionLabel>
                    {item.source} / {item.tags[0] || "Saved article"}
                  </SectionLabel>
                  <h2 className="font-display mt-5 text-[34px] leading-[1.12] sm:text-[42px]">
                    {item.title}
                  </h2>
                  <p className="font-display mt-5 text-xl leading-7 text-[var(--muted)]">
                    {item.excerpt}
                  </p>
                  <div className="mb-7 mt-6 flex flex-wrap gap-4 border-b border-[var(--line)] pb-5 text-xs">
                    {item.author && <span>{item.author}</span>}
                    <span className="text-[var(--muted)]">
                      {dateLabel(item.saved)}
                    </span>
                  </div>
                  {item.versions.length ? (
                    <div className="font-display space-y-6 text-[19px] leading-8">
                      {prose.map((t) => (
                        <p key={t}>{t}</p>
                      ))}
                    </div>
                  ) : (
                    <div className="rounded bg-[var(--warning-soft)] p-6 text-sm leading-6">
                      Your link is saved. A reading version will become
                      available after capture is connected to the server.
                    </div>
                  )}
                  <p className="mt-10 border-t border-[var(--line)] pt-4 text-[11px] leading-5 text-[var(--muted)]">
                    Preview text supplied for the design. Server integration
                    will load the preserved article.
                  </p>
                </Tabs.Content>
                <Tabs.Content value="original" className="p-6 sm:p-12">
                  <Badge tone="warning">Static saved-page preview</Badge>
                  <h2 className="font-display mt-7 text-4xl">{item.title}</h2>
                  <p className="my-6 max-w-xl text-sm leading-7">
                    {selected
                      ? `Viewing the ${selected.status.toLowerCase()} copy dated ${dateLabel(selected.date)}. Interactive site features are not preserved.`
                      : "No snapshot is available for this item yet."}
                  </p>
                  <div className="border-y border-[var(--line)] py-10 font-display text-xl leading-9">
                    {prose[0]}
                  </div>
                  <p className="mt-6 text-xs leading-6 text-[var(--muted)]">
                    Layout simulation. Actual saved-page content will be
                    connected in the integration phase.
                  </p>
                </Tabs.Content>
              </>
            )}
          </div>
          <aside
            id="item-information"
            aria-label="Item information"
            hidden={!informationOpen}
            className="space-y-6 p-3 text-xs"
          >
            <div className="flex items-center justify-between gap-2">
              <h2 className="font-display text-xl">Item information</h2>
              <Button
                aria-label="Hide item information"
                aria-expanded={true}
                aria-controls="item-information"
                onClick={() => setInformationOpen(false)}
              >
                <PanelRightClose size={18} />
              </Button>
            </div>
            {!isPdf && (
              <section>
                <SectionLabel>Language</SectionLabel>
                <p className="mt-3 text-[11px] leading-5 text-[var(--muted)]">
                  {item.outputLanguage
                    ? `Reading in ${languages.find(([code]) => code === item.outputLanguage)?.[1] || item.outputLanguage}.`
                    : "Reading the original language."}{" "}
                  Translate the extracted article and its PDF.
                </p>
                <label className="mt-3 block">
                  <span className="sr-only">Translation language</span>
                  <select
                    className={`${input} min-h-11`}
                    value={language}
                    onChange={(e) => setLanguage(e.target.value)}
                  >
                    {languages.map(([code, label]) => (
                      <option key={code} value={code}>
                        {label}
                      </option>
                    ))}
                  </select>
                </label>
                <div className="mt-2 flex flex-wrap gap-2">
                  <Button
                    disabled={
                      translating ||
                      (!archive.preview &&
                        !item.textAvailable &&
                        !item.readingAvailable)
                    }
                    onClick={() => void translate(language)}
                  >
                    {translating ? "Updating…" : "Translate"}
                  </Button>
                  {item.outputLanguage && (
                    <Button
                      disabled={translating}
                      onClick={() => void translate("")}
                    >
                      Original
                    </Button>
                  )}
                </div>
                {translationError && (
                  <p role="alert" className="mt-2 text-[var(--danger)]">
                    {translationError}
                  </p>
                )}
                {!archive.preview &&
                  !item.textAvailable &&
                  !item.readingAvailable && (
                    <p className="mt-2 text-[11px] text-[var(--muted)]">
                      Available after article text is extracted.
                    </p>
                  )}
              </section>
            )}
            {!isPdf && (
              <section>
                <SectionLabel>Reading PDF</SectionLabel>
                <div className="mt-3">
                  {hasPdf ? (
                    <Button onClick={download} disabled={!file}>
                      <Download size={15} />
                      Download PDF
                    </Button>
                  ) : (
                    <Button
                      onClick={() => generate(item.id)}
                      disabled={["queued", "processing"].includes(
                        item.pdfStatus || "",
                      )}
                    >
                      <RefreshCw size={15} />
                      {["queued", "processing"].includes(item.pdfStatus || "")
                        ? "Generating PDF…"
                        : "Generate PDF"}
                    </Button>
                  )}
                  {item.pdfStatus === "failed" && !hasPdf && (
                    <p role="alert" className="mt-2 text-[var(--danger)]">
                      PDF generation failed. You can try again.
                    </p>
                  )}
                  {!hasPdf && (
                    <p className="mt-2 text-[11px] text-[var(--muted)]">
                      Prepare a reading document without sending email.
                    </p>
                  )}
                </div>
              </section>
            )}
            {!isPdf && (
              <section>
                <SectionLabel>Capture context</SectionLabel>
                <div
                  className={`mt-3 rounded p-4 leading-6 ${item.capture === "Capture failed" ? "bg-[var(--danger-soft)] text-[var(--danger)]" : item.capture === "Preserved" ? "bg-[var(--success-soft)] text-[var(--success)]" : "bg-[var(--warning-soft)] text-[var(--warning)]"}`}
                >
                  <p className="flex items-center gap-2 font-medium">
                    <CheckCircle2 size={15} />
                    {item.capture}
                  </p>
                  <p className="mt-1 text-[11px]">
                    {item.capture === "Needs browser capture"
                      ? "Choose Continue capture to recover the page here, including on mobile. You can also save an open page with the Chromium extension."
                      : item.capture === "Preserved" &&
                          item.versions[version]?.status === "Partial"
                        ? "Content extracted and preserved. Some page resources could not be saved; details are listed below."
                        : item.capture === "Not captured"
                          ? "This bookmark has not been captured. Request a capture to preserve a local copy."
                          : item.versions.length
                            ? "Earlier saved copies remain available, even if a new capture fails."
                            : "The bookmark is kept. Capture does not determine whether it stays saved."}
                  </p>
                </div>
              </section>
            )}
            <section>
              <SectionLabel>Tags</SectionLabel>
              <div className="mt-3 flex flex-wrap gap-2">
                {item.tags.length ? (
                  item.tags.map((t) => (
                    <span
                      key={t}
                      className="rounded-sm bg-[var(--accent-soft)] px-2 py-1 text-[10px] text-[var(--accent-ink)]"
                    >
                      {t}
                    </span>
                  ))
                ) : (
                  <span className="text-[var(--muted)]">No tags yet</span>
                )}
              </div>
            </section>
            {!isPdf && (
              <section>
                <SectionLabel>Versions</SectionLabel>
                <div className="mt-3">
                  {item.versions.map((v, index) => (
                    <button
                      key={`${v.date}-${index}`}
                      onClick={() => setVersion(index)}
                      className={`flex w-full items-center justify-between gap-2 border-b border-[var(--line)] px-2 py-3 text-left ${index === version ? "bg-[var(--paper)]" : ""}`}
                    >
                      <span>
                        {dateLabel(v.date)}
                        <span className="mt-1 block text-[10px] text-[var(--muted)]">
                          {v.source === "browser"
                            ? "Captured in your browser"
                            : v.source === "server"
                              ? "Captured by Attic"
                              : "Retained snapshot"}
                        </span>
                      </span>
                      <Badge tone="success">Preserved</Badge>
                    </button>
                  ))}
                </div>
                {!!item.versions[version]?.missing?.length && (
                  <details className="mt-3 text-[11px] text-[var(--muted)]">
                    <summary>
                      Missing resources (
                      {item.versions[version].missing!.length})
                    </summary>
                    <ul className="mt-2 space-y-2 break-all">
                      {item.versions[version].missing!.map((resource) => (
                        <li key={resource}>{resource}</li>
                      ))}
                    </ul>
                  </details>
                )}
                <p className="my-4 text-[11px] leading-5 text-[var(--muted)]">
                  A fresh capture keeps every earlier version. The original site
                  may have changed.
                </p>
                <Button
                  disabled={["Preserving", "Capture queued"].includes(
                    item.capture,
                  )}
                  onClick={() => recapture(item.id)}
                >
                  <RefreshCw size={14} />
                  {["Preserving", "Capture queued"].includes(item.capture)
                    ? "Capture queued"
                    : "Request fresh capture"}
                </Button>
              </section>
            )}
            <section>
              <SectionLabel>Kindle delivery</SectionLabel>
              <div className="mt-3 rounded border border-[var(--line)] bg-[var(--paper)] p-4">
                <p className="font-medium">{item.delivery}</p>
                {item.delivery !== "Email Sent" && (
                  <p className="mt-2 text-[11px] leading-5 text-[var(--muted)]">
                    {item.delivery === "Preparation failed"
                      ? "Document preparation failed. Your saved copy is safe; you can try again."
                      : item.delivery === "Outcome uncertain"
                        ? "The email acknowledgement was interrupted. Resending may create a duplicate."
                        : item.delivery === "Delivery failed"
                          ? "The email service could not accept the document. Your saved copy is safe."
                          : item.delivery === "Preparing document"
                            ? "Your item is saved. Document preparation and delivery are separate steps."
                            : "Send this item when you want to read it on your Kindle."}
                  </p>
                )}
                {item.delivery === "Email Sent" && (
                  <Button className="mt-3" onClick={requestSend}>
                    Resend to Kindle
                  </Button>
                )}
                {[
                  "Preparation failed",
                  "Delivery failed",
                  "Outcome uncertain",
                ].includes(item.delivery) && (
                  <Button className="mt-3" onClick={requestSend}>
                    Retry delivery
                  </Button>
                )}
              </div>
            </section>
          </aside>
        </div>
      </Tabs.Root>
      {recoveryOpen && (
        <BrowserRecovery
          itemID={item.id}
          onClose={() => setRecoveryOpen(false)}
        />
      )}
      <Modal
        open={details}
        onOpenChange={setDetails}
        title="Item details"
        wide
        description={`${item.kind.toUpperCase()} · ${item.source}`}
      >
        <form
          className="grid gap-5"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await archive.edit({
                ...item,
                title: title.trim() || item.title,
                notes,
                tags: [
                  ...new Set(
                    tags
                      .split(",")
                      .map((t) => t.trim())
                      .filter(Boolean),
                  ),
                ],
              });
              refresh();
              setDetails(false);
              notify("Title, notes and tags saved.");
            } catch (e) {
              notify((e as Error).message);
            }
          }}
        >
          <label className="grid gap-2 text-xs">
            Title
            <input
              required
              autoFocus
              maxLength={1000}
              className={input}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </label>
          {safe && (
            <a
              href={safe}
              target="_blank"
              rel="noreferrer"
              className="break-all text-xs text-[var(--muted)] underline"
            >
              {item.url}
            </a>
          )}
          <dl className="grid grid-cols-[100px_1fr] gap-3 text-xs">
            <dt className="text-[var(--muted)]">Author</dt>
            <dd>{item.author || "Not available"}</dd>
            <dt className="text-[var(--muted)]">Saved</dt>
            <dd>{dateLabel(item.saved)}</dd>
            {item.folder && (
              <>
                <dt className="text-[var(--muted)]">Browser folder</dt>
                <dd>{item.folder}</dd>
              </>
            )}
          </dl>
          <label className="grid gap-2 text-xs">
            Notes
            <textarea
              rows={3}
              className={input}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </label>
          <label className="grid gap-2 text-xs">
            Tags (comma separated)
            <input
              className={input}
              value={tags}
              onChange={(e) => setTags(e.target.value)}
            />
          </label>

          <div className="flex flex-wrap justify-between gap-3 border-t border-[var(--line)] pt-5">
            <Button
              type="button"
              className="text-[var(--danger)]"
              onClick={() => setRemove(true)}
            >
              <Trash2 size={14} />
              Remove item
            </Button>
            <Button primary type="submit">
              Save changes
            </Button>
          </div>
        </form>
      </Modal>
      <Modal
        open={remove}
        onOpenChange={setRemove}
        title="Remove this item?"
        description="You can undo removal for 10 seconds. Browser bookmarks are not changed."
      >
        <p className="font-display text-xl">{item.title}</p>
        <div className="mt-6 flex justify-end gap-3">
          <Button onClick={() => setRemove(false)}>Keep item</Button>
          <Button
            className="bg-[var(--danger)]! text-[var(--paper)]"
            onClick={() => {
              removeItems([item.id]);
              navigate("/");
            }}
          >
            Remove item
          </Button>
        </div>
      </Modal>
      <Modal
        open={resend}
        onOpenChange={setResend}
        title="Send this document again?"
        description="A previous attempt may already have reached your Kindle. Sending again may create a duplicate."
      >
        <div className="flex justify-end gap-3">
          <Button onClick={() => setResend(false)}>Cancel</Button>
          <Button
            primary
            onClick={() => {
              send(item.id);
              setResend(false);
            }}
          >
            Send again
          </Button>
        </div>
      </Modal>
    </main>
  );
}
