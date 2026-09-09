import { useId, useState } from "react";
import { useArchive, useArchiveQuery } from "../state";
import { input } from "./primitives";

export function FilterAutocomplete({
  field,
  label,
  value,
  onChange,
}: {
  field: "tag" | "source";
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { archive } = useArchive();
  const id = useId();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const { data = [], error } = useArchiveQuery(
    `suggestions:${field}:${value}`,
    (signal) => archive.suggestions(field, value, signal),
  );
  function choose(value: string) {
    onChange(value);
    setOpen(false);
    setActive(-1);
  }
  return (
    <label className="relative grid gap-2 text-xs">
      {label}
      <input
        className={input}
        role="combobox"
        aria-label={label}
        autoComplete="off"
        value={value}
        aria-autocomplete="list"
        aria-expanded={open && !!data.length}
        aria-controls={id}
        aria-activedescendant={
          open && active >= 0 && active < data.length
            ? `${id}-${active}`
            : undefined
        }
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onChange={(event) => {
          onChange(event.target.value);
          setOpen(true);
          setActive(-1);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            if (open) event.stopPropagation();
            setOpen(false);
          }
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            setOpen(true);
            setActive((previous) =>
              Math.max(
                0,
                Math.min(
                  data.length - 1,
                  previous + (event.key === "ArrowDown" ? 1 : -1),
                ),
              ),
            );
          }
          if (event.key === "Enter" && open && data[active]) {
            event.preventDefault();
            choose(data[active]);
          }
        }}
      />
      {open && !!data.length && (
        <ul
          id={id}
          role="listbox"
          aria-label={`${label} suggestions`}
          className="absolute top-full z-20 max-h-52 w-full overflow-auto rounded border border-[var(--line)] bg-[var(--paper)] shadow-lg"
        >
          {data.map((value, index) => (
            <li
              id={`${id}-${index}`}
              key={value}
              role="option"
              aria-selected={active === index}
              className={`cursor-pointer break-all px-3 py-3 hover:bg-[var(--accent-soft)] ${active === index ? "bg-[var(--accent-soft)]" : ""}`}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => choose(value)}
            >
              {value}
            </li>
          ))}
        </ul>
      )}
      {error && (
        <span role="status" className="text-[var(--muted)]">
          Suggestions unavailable
        </span>
      )}
    </label>
  );
}
