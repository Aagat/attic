import { createContext, useContext, useEffect, useState } from "react";
import type { Item } from "./model";
import type { Archive } from "./archive/contract";
import { createHTTPArchive } from "./archive/http";
import { createPreviewArchive } from "./archive/preview";
export const archive =
  import.meta.env.VITE_UI_PREVIEW === "true"
    ? createPreviewArchive()
    : createHTTPArchive();
export interface ArchiveState {
  archive: Archive;
  revision: number;
  refresh: () => void;
  login: (key: string) => Promise<void>;
  logout: () => Promise<void>;
  notify: (message: string) => void;
  save: (url: string, send: boolean, key: string) => Promise<Item>;
  openSave: () => void;
  openUpload: () => void;
  send: (id: string) => void;
  recapture: (id: string) => void;
  remove: (ids: string[]) => void;
  hiddenItems: string[];
  generate: (id: string) => void;
}
export const ArchiveContext = createContext<ArchiveState>(null!);
export const useArchive = () => useContext(ArchiveContext);
export function useArchiveQuery<T>(
  key: string,
  query: (signal: AbortSignal) => Promise<T>,
) {
  const { revision } = useArchive();
  const [state, setState] = useState<{ key: string; data?: T; error?: string }>(
    { key },
  );
  useEffect(() => {
    let active = true;
    let controller: AbortController;
    let timer: ReturnType<typeof setTimeout>;
    async function run() {
      controller = new AbortController();
      try {
        const data = await query(controller.signal);
        if (active) setState({ key, data });
      } catch (e) {
        if (active && (e as Error).name !== "AbortError")
          setState((p) => ({
            key,
            data: p.key === key ? p.data : undefined,
            error: (e as Error).message,
          }));
      } finally {
        if (active) timer = setTimeout(run, 5000);
      }
    }
    setState((p) => (p.key === key ? p : { key }));
    timer = setTimeout(run, 200);
    return () => {
      active = false;
      clearTimeout(timer);
      controller?.abort();
    };
    // The key fully describes query inputs; revision refreshes mutations.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, revision]);
  return state.key === key ? state : { key };
}
