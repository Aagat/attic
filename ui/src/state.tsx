import { createContext, useContext } from "react";
import type { Item } from "./model";
export interface PreviewState {
  items: Item[];
  setItems: (action: React.SetStateAction<Item[]>) => Promise<void>;
  notify: (message: string) => void;
  save: (url: string, send: boolean) => Item;
  openSave: () => void;
  openUpload: () => void;
  send: (id: string) => void;
  recapture: (id: string) => void;
}
export const PreviewContext = createContext<PreviewState>(null!);
export const usePreview = () => useContext(PreviewContext);
