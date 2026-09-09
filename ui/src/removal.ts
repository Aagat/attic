import { useEffect, useRef, useState } from "react";
import { applyToItems } from "./batch";
import type { Archive } from "./archive/contract";

// Delay destructive work so undo retains captures, PDFs, and bookmark identity.
export function useRemoval(
  archive: Archive,
  refresh: () => void,
  notify: (message: string) => void,
) {
  const [hidden, setHidden] = useState<string[]>([]);
  const [pending, setPending] = useState<string[]>([]);
  const batch = useRef<string[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  function remove(ids: string[]) {
    batch.current = [...new Set([...batch.current, ...ids])];
    setHidden((previous) => [...new Set([...previous, ...ids])]);
    setPending([...batch.current]);
    clearTimeout(timer.current);
    timer.current = setTimeout(async () => {
      const ids = batch.current;
      batch.current = [];
      setPending([]);
      const failed = await applyToItems(ids, (id) => archive.remove(id));
      // Refresh server state before revealing failed removals. Successfully removed
      // IDs stay hidden until this app session ends, including during stale polls.
      refresh();
      setHidden((previous) => previous.filter((id) => !failed.includes(id)));
      if (failed.length)
        notify(`${failed.length} item(s) could not be removed. Please retry.`);
    }, 10000);
  }
  function undo() {
    clearTimeout(timer.current);
    const ids = batch.current;
    batch.current = [];
    setPending([]);
    setHidden((previous) => previous.filter((id) => !ids.includes(id)));
    refresh();
  }
  return { hidden, pending, remove, undo };
}
