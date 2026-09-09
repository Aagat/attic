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
export interface RecoverySession {
  status: "idle" | "starting" | "working" | "needs_input" | "saved" | "failed";
  url: string;
  title: string;
  message: string;
  width: number;
  height: number;
  screenshot: string;
}
export interface RecoveryAction {
  action:
    | "start"
    | "takeover"
    | "click"
    | "type"
    | "scroll"
    | "key"
    | "capture"
    | "close";
  x?: number;
  y?: number;
  text?: string;
  delta_y?: number;
}
export interface Archive {
  recovery(
    id: string,
    action?: RecoveryAction,
    signal?: AbortSignal,
  ): Promise<RecoverySession>;
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
    action: "send" | "recapture" | "enrich" | "generate",
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
  translate(id: string, language: string): Promise<Item>;
  readingURL(item: Item, signal?: AbortSignal): Promise<string>;
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
