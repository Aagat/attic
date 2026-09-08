import type { ButtonHTMLAttributes, ReactNode } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { X, Archive } from "lucide-react";
import { Link } from "react-router-dom";
export const border = "border-[var(--line)]";
export const input =
  "w-full rounded border border-[var(--line)] bg-[var(--surface)] px-3 py-3 text-sm";
export function Button({
  primary = false,
  children,
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { primary?: boolean }) {
  return (
    <button
      {...props}
      className={`inline-flex scroll-mb-28 scroll-mt-24 min-h-11 items-center justify-center gap-2 rounded border px-3.5 py-2 text-[13px] font-medium transition-colors disabled:opacity-45 motion-reduce:transition-none ${primary ? "border-[var(--accent)] bg-[var(--accent)] text-[var(--ink)] hover:bg-[var(--accent-soft)]" : "border-[var(--line)] bg-[var(--paper)] hover:bg-[var(--surface)]"} ${className}`}
    >
      {children}
    </button>
  );
}
export function Brand() {
  return (
    <Link
      to="/"
      aria-label="Attic library"
      className="inline-flex items-center gap-2.5"
    >
      <Archive className="size-5 text-[var(--accent)]" />
      <span className="font-display text-[30px] leading-none">attic</span>
    </Link>
  );
}
export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  wide = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  wide?: boolean;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-[var(--ink)]/35 backdrop-blur-[2px]" />
        <Dialog.Content
          className={`fixed left-1/2 top-1/2 z-50 max-h-[90dvh] w-[calc(100%-32px)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-lg border border-[var(--line)] bg-[var(--paper)] p-6 shadow-xl ${wide ? "max-w-2xl" : "max-w-lg"}`}
        >
          <Dialog.Title className="font-display pr-10 text-3xl">
            {title}
          </Dialog.Title>
          <Dialog.Description
            className={
              description
                ? "mt-2 text-sm leading-6 text-[var(--muted)]"
                : "sr-only"
            }
          >
            {description || title}
          </Dialog.Description>
          <Dialog.Close asChild>
            <Button
              className="absolute right-3 top-3 border-transparent"
              aria-label="Close dialog"
            >
              <X size={18} />
            </Button>
          </Dialog.Close>
          <div className="mt-6">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
export function Badge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "success" | "warning" | "danger";
}) {
  const styles = {
    neutral: "bg-[var(--surface)] text-[var(--muted)]",
    success: "bg-[var(--success-soft)] text-[var(--success)]",
    warning: "bg-[var(--warning-soft)] text-[var(--warning)]",
    danger: "bg-[var(--danger-soft)] text-[var(--danger)]",
  };
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-1 text-[11px] font-medium ${styles[tone]}`}
    >
      {children}
    </span>
  );
}
export function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <h3 className="text-[10px] font-medium uppercase tracking-[.14em] text-[var(--muted)]">
      {children}
    </h3>
  );
}
