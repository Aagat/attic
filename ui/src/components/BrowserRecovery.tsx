import { useEffect, useRef, useState } from "react";
import type { RecoveryAction, RecoverySession } from "../archive/contract";
import { useArchive } from "../state";
import { Button, Modal, input } from "./primitives";

// Own the session lifecycle here so polling never races browser input or repeats
// an AI attempt as the reader refreshes its item data.
export function BrowserRecovery({
  itemID,
  onClose,
}: {
  itemID: string;
  onClose: () => void;
}) {
  const { archive, refresh, notify } = useArchive();
  const [session, setSession] = useState<RecoverySession>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [text, setText] = useState("");
  const [secret, setSecret] = useState(false);
  const controller = useRef<AbortController | null>(null);
  const pending = useRef(false);
  const actionActive = useRef(false);
  const pollDone = useRef<Promise<void>>(Promise.resolve());
  const saved = useRef(false);
  const terminal = useRef(false);
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  const accept = useRef((value: RecoverySession) => {
    terminal.current = value.status === "saved" || value.status === "failed";
    setSession((previous) => ({
      ...value,
      screenshot: value.screenshot || previous?.screenshot || "",
    }));
    if (value.status === "saved" && !saved.current) {
      saved.current = true;
      refreshRef.current();
    }
  });

  useEffect(() => {
    const abort = new AbortController();
    controller.current = abort;
    let first = true;
    async function poll() {
      if (
        pending.current ||
        actionActive.current ||
        terminal.current ||
        abort.signal.aborted
      )
        return;
      pending.current = true;
      let finish!: () => void;
      pollDone.current = new Promise((resolve) => {
        finish = resolve;
      });
      try {
        let value = await archive.recovery(itemID, undefined, abort.signal);
        if (first && value.status === "idle") {
          first = false;
          value = await archive.recovery(
            itemID,
            { action: "start" },
            abort.signal,
          );
        }
        first = false;
        if (!abort.signal.aborted) {
          accept.current(value);
          setError("");
        }
      } catch (cause) {
        first = false;
        if (!abort.signal.aborted) setError((cause as Error).message);
      } finally {
        pending.current = false;
        finish();
      }
    }
    void poll();
    const timer = setInterval(() => void poll(), 2000);
    return () => {
      abort.abort();
      clearInterval(timer);
    };
  }, [archive, itemID]);

  async function act(action: RecoveryAction) {
    if (actionActive.current || controller.current?.signal.aborted) return;
    actionActive.current = true;
    setBusy(true);
    setError("");
    try {
      await pollDone.current;
      if (controller.current?.signal.aborted) return;
      const value = await archive.recovery(
        itemID,
        action,
        controller.current?.signal,
      );
      if (!controller.current?.signal.aborted) accept.current(value);
    } catch (cause) {
      if (!controller.current?.signal.aborted)
        setError((cause as Error).message);
    } finally {
      actionActive.current = false;
      setBusy(false);
    }
  }
  function close() {
    controller.current?.abort();
    setText("");
    void archive
      .recovery(itemID, { action: "close" })
      .catch((cause) => notify((cause as Error).message));
    onClose();
  }
  const working =
    session?.status === "working" || session?.status === "starting";
  const interactive = session?.status === "needs_input" && !busy;
  const screenshot = session?.screenshot.startsWith("data:image/png;base64,")
    ? session.screenshot
    : "";
  return (
    <Modal
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
      title="Continue capture"
      wide
      description="Attic tries to recover this page. If the site needs your help, control the browser here. Earlier copies stay saved."
    >
      <div className="space-y-4">
        <p role="status" className="text-sm leading-6">
          {session?.message || "Opening capture session…"}
        </p>
        {session?.url && (
          <p className="break-all text-xs text-[var(--muted)]">{session.url}</p>
        )}
        {error && (
          <p role="alert" className="text-sm text-[var(--danger)]">
            {error}
          </p>
        )}
        {working && (
          <Button
            disabled={busy}
            onClick={() => void act({ action: "takeover" })}
          >
            Take control
          </Button>
        )}
        {(session?.status === "failed" || session?.status === "idle") && (
          <Button
            disabled={busy}
            onClick={() => {
              saved.current = false;
              void act({ action: "start" });
            }}
          >
            Try recovery again
          </Button>
        )}
        {screenshot && (
          <div
            className="overflow-hidden rounded border border-[var(--line)] bg-white"
            style={{
              aspectRatio: `${session?.width || 1024} / ${session?.height || 768}`,
            }}
          >
            <img
              src={screenshot}
              alt="Recovery browser page"
              draggable={false}
              className={`block h-auto w-full ${interactive ? "cursor-crosshair" : ""}`}
              onClick={(event) => {
                if (!interactive || !session) return;
                const rect = event.currentTarget.getBoundingClientRect();
                void act({
                  action: "click",
                  x: Math.min(
                    session.width - 1,
                    Math.max(
                      0,
                      Math.floor(
                        ((event.clientX - rect.left) * session.width) /
                          rect.width,
                      ),
                    ),
                  ),
                  y: Math.min(
                    session.height - 1,
                    Math.max(
                      0,
                      Math.floor(
                        ((event.clientY - rect.top) * session.height) /
                          rect.height,
                      ),
                    ),
                  ),
                });
              }}
            />
          </div>
        )}
        {session?.status === "needs_input" && (
          <>
            <p className="text-xs leading-5 text-[var(--muted)]">
              Tap the page to interact. Complete verification or sign in
              yourself, then save the page. This session expires after
              inactivity.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={!interactive}
                onClick={() => void act({ action: "scroll", delta_y: -500 })}
              >
                Scroll up
              </Button>
              <Button
                disabled={!interactive}
                onClick={() => void act({ action: "scroll", delta_y: 500 })}
              >
                Scroll down
              </Button>
              {["Tab", "Enter", "Backspace"].map((key) => (
                <Button
                  key={key}
                  disabled={!interactive}
                  onClick={() => void act({ action: "key", text: key })}
                >
                  {key}
                </Button>
              ))}
            </div>
            <form
              className="space-y-2"
              onSubmit={(event) => {
                event.preventDefault();
                if (text && interactive && !actionActive.current) {
                  const value = text;
                  setText("");
                  void act({ action: "type", text: value });
                }
              }}
            >
              <label className="grid gap-2 text-xs">
                Text for the selected field
                <input
                  className={input}
                  type={secret ? "password" : "text"}
                  value={text}
                  onChange={(event) => setText(event.target.value)}
                  autoComplete="off"
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </label>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <label className="flex min-h-11 items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    checked={secret}
                    onChange={(event) => setSecret(event.target.checked)}
                  />
                  Hide sensitive text
                </label>
                <Button type="submit" disabled={!interactive || !text}>
                  Send text
                </Button>
              </div>
            </form>
            <Button
              primary
              disabled={!interactive}
              onClick={() => void act({ action: "capture" })}
            >
              Save this page
            </Button>
          </>
        )}
        {session?.status === "saved" && (
          <Button primary onClick={close}>
            Return to saved item
          </Button>
        )}
      </div>
    </Modal>
  );
}
