import { useState } from "react";
import type { Item } from "../model";
import { applyToItems } from "../batch";
import { randomId } from "../id";
import { useArchive } from "../state";
import { Button, Modal, input } from "./primitives";

export function BulkActions({
  items,
  selected,
  setSelected,
  busy,
  setBusy,
}: {
  items: Item[];
  selected: string[];
  setSelected: (ids: string[]) => void;
  busy: boolean;
  setBusy: (busy: boolean) => void;
}) {
  const { archive, refresh, notify, remove } = useArchive();
  const [tags, setTags] = useState("");
  const [confirm, setConfirm] = useState(false);
  const retryable = items.filter(
    (item) =>
      selected.includes(item.id) &&
      ["Capture failed", "Needs browser capture", "Not captured"].includes(
        item.capture,
      ),
  );
  async function run(action: "tags" | "send" | "recapture") {
    const ids =
      action === "recapture" ? retryable.map((item) => item.id) : [...selected];
    setBusy(true);
    const failed = await applyToItems(ids, async (id) => {
      if (action === "tags") {
        const item = await archive.get(id);
        await archive.edit({
          ...item,
          tags: [
            ...new Set([
              ...item.tags,
              ...tags
                .split(",")
                .map((tag) => tag.trim())
                .filter(Boolean),
            ]),
          ],
        });
      } else await archive.act(id, action, randomId());
    });
    setSelected(failed);
    setBusy(false);
    refresh();
    notify(
      `${ids.length - failed.length} updated${failed.length ? ` · ${failed.length} failed; selected for retry` : ""}.`,
    );
  }
  return (
    <div className="mb-4 flex flex-wrap items-center gap-3 rounded border border-[var(--line)] bg-[var(--paper)] p-3 text-xs">
      <label className="flex min-h-11 items-center gap-2">
        <input
          type="checkbox"
          aria-label="Select this page"
          disabled={busy || !items.length}
          checked={
            !!items.length && items.every((item) => selected.includes(item.id))
          }
          onChange={(event) =>
            setSelected(
              event.target.checked ? items.map((item) => item.id) : [],
            )
          }
        />
        {selected.length ? `${selected.length} selected` : "Select"}
      </label>
      {busy && <span role="status">Working…</span>}
      {!!selected.length && (
        <>
          <Button disabled={busy} onClick={() => void run("send")}>
            Send to Kindle
          </Button>
          <Button
            disabled={busy || !retryable.length}
            onClick={() => void run("recapture")}
          >
            Retry captures
          </Button>
          <input
            className={input + " min-w-0 w-40"}
            aria-label="Tags to add"
            placeholder="Tags, comma separated"
            value={tags}
            disabled={busy}
            onChange={(event) => setTags(event.target.value)}
          />
          <Button
            disabled={busy || !tags.split(",").some((tag) => tag.trim())}
            onClick={() => void run("tags")}
          >
            Add tags
          </Button>
          <Button disabled={busy} onClick={() => setConfirm(true)}>
            Remove selected
          </Button>
          <Button disabled={busy} onClick={() => setSelected([])}>
            Clear selection
          </Button>
        </>
      )}
      <Modal
        open={confirm}
        onOpenChange={setConfirm}
        title={`Remove ${selected.length} items?`}
        description="You can undo removal for 10 seconds."
      >
        <div className="flex justify-end gap-3">
          <Button onClick={() => setConfirm(false)}>Cancel</Button>
          <Button
            onClick={() => {
              remove(selected);
              setSelected([]);
              setConfirm(false);
            }}
          >
            Remove items
          </Button>
        </div>
      </Modal>
    </div>
  );
}
