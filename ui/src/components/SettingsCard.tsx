import type { LucideIcon } from "lucide-react";
import { Button } from "./primitives";

export function SettingsCard({
  title,
  body,
  label,
  icon: Icon,
  action,
  disabled,
  primary = false,
}: {
  title: string;
  body: string;
  label: string;
  icon: LucideIcon;
  action: () => void;
  disabled?: boolean;
  primary?: boolean;
}) {
  return (
    <section className="flex min-h-[230px] flex-col items-start border border-[var(--line)] bg-[var(--paper)] p-5">
      <Icon size={20} aria-hidden="true" />
      <h3 className="font-display mt-4 text-2xl">{title}</h3>
      <p className="mb-6 mt-3 text-xs leading-6 text-[var(--muted)]">{body}</p>
      <Button
        primary={primary}
        className="mt-auto"
        disabled={disabled}
        onClick={action}
      >
        {label}
      </Button>
    </section>
  );
}
