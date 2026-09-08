import type { Item } from "../model";
export interface LibraryPage {
  items: Item[];
  total: number;
  storageBytes: number;
  estimated?: boolean;
}
export interface ArchiveStatus {
  total: number;
  storageBytes: number;
  kindle: boolean;
  search: boolean;
}
export interface Archive {
  readonly preview: boolean;
  session(key?: string): Promise<void>;
  signOut(): Promise<void>;
  list(params: URLSearchParams, signal?: AbortSignal): Promise<LibraryPage>;
  get(id: string, signal?: AbortSignal): Promise<Item>;
  save(url: string, kindle: boolean, key: string): Promise<Item>;
  edit(item: Item): Promise<void>;
  remove(id: string): Promise<void>;
  act(
    id: string,
    action: "send" | "recapture" | "enrich",
    key: string,
  ): Promise<void>;
  upload(file: File, kindle: boolean, key: string): Promise<Item>;
  transfer(action: "import" | "restore", file: File): Promise<string>;
  export(): Promise<{ url: string; filename: string; release?: () => void }>;
  captureURL(
    item: Item,
    index: number,
    reading: boolean,
    signal?: AbortSignal,
  ): Promise<string>;
  pdf(item: Item): Promise<File>;
  status(): Promise<ArchiveStatus>;
  reset?(items: Item[]): Promise<void>;
}
export class ArchiveError extends Error {
  constructor(
    message: string,
    public status = 0,
  ) {
    super(message);
    this.name = "ArchiveError";
  }
}
