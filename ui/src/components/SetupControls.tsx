import { useEffect, useState } from "react";
import { useArchive } from "../state";
import { Modal, Button, input } from "./primitives";
import { useSearchParams } from "react-router-dom";
import { Sparkles, Send, FileCheck, Search } from "lucide-react";
import { SettingsCard } from "./SettingsCard";

type Mail = {
  enabled: boolean;
  host: string;
  port: number;
  tls_mode: string;
  username: string;
  sender: string;
  destination: string;
  password_set: boolean;
  managed: boolean;
};
type Setup = {
  provider: string;
  ai: string;
  login: { state: string; url?: string; code?: string };
  mail: Mail;
  compatibility: string;
  runtime?: Record<string, string>;
  search_configured: boolean;
};
export function SetupControls() {
  const { archive, refresh } = useArchive();
  const [params, setParams] = useSearchParams();
  const panel = params.get("panel");
  function openPanel(value: string | null) {
    setMessage("");
    setPassword("");
    if (status)
      setMail({
        ...status.mail,
        tls_mode: status.mail.tls_mode || "starttls",
        port: status.mail.port || 587,
      });
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (value) next.set("panel", value);
        else next.delete("panel");
        return next;
      },
      { replace: true },
    );
  }
  const [status, setStatus] = useState<Setup>();
  const [mail, setMail] = useState<Mail>();
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [runtime, setRuntime] = useState<Record<string, string>>();
  const [search, setSearch] = useState<{
    state: string;
    pending: number;
    failed: number;
  }>();
  useEffect(() => {
    let active = true;
    const read = async () => {
      try {
        const s = await archive.setup<Setup>();
        if (active) {
          setStatus(s);
          if (s.runtime) setRuntime(s.runtime);
          setMail(
            (m) =>
              m ?? {
                ...s.mail,
                tls_mode: s.mail.tls_mode || "starttls",
                port: s.mail.port || 587,
              },
          );
        }
      } catch (e) {
        if (active) setMessage((e as Error).message);
      }
    };
    void read();
    const timer = setInterval(read, 3000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [archive]);
  async function act(path: string, method = "POST", body?: unknown) {
    setBusy(true);
    setMessage("");
    try {
      const r = await archive.setup<Record<string, string>>(path, method, body);
      if (path === "/runtime") setRuntime(r);
      else if (path === "/search") setSearch(r as unknown as typeof search);
      else setMessage(r.message ?? "Saved. No email was sent.");
      const s = await archive.setup<Setup>();
      setStatus(s);
      if (path === "/mail") {
        setMail(s.mail);
        setPassword("");
      }
      refresh();
    } catch (e) {
      setMessage((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <h2 className="font-display mt-9 mb-6 text-2xl">Connections and tools</h2>
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {[
          {
            id: "chatgpt",
            title: "ChatGPT",
            body: "Connect your account to prepare and enrich saved articles.",
            label: "Manage ChatGPT",
            icon: Sparkles,
          },
          {
            id: "mail",
            title: "Kindle delivery",
            body: "Choose how reading documents reach your Kindle.",
            label: "Manage delivery",
            icon: Send,
          },
          {
            id: "runtime",
            title: "PDF and browser",
            body: "Check the tools that preserve pages and prepare PDFs.",
            label: "View runtime",
            icon: FileCheck,
          },
          {
            id: "search",
            title: "Search index",
            body: "Check indexing progress or rebuild search from saved items.",
            label: "Manage search",
            icon: Search,
          },
        ].map(({ id, ...card }) => (
          <SettingsCard
            key={id}
            {...card}
            disabled={busy}
            action={() => openPanel(id)}
          />
        ))}
      </div>
      <Modal
        open={panel === "chatgpt"}
        onOpenChange={(open) => {
          if (!open) openPanel(null);
        }}
        title="ChatGPT"
        description="Connect your account and check AI compatibility."
      >
        <div className="space-y-4 text-sm leading-6 text-[var(--muted)]">
          <h3>
            ChatGPT ·{" "}
            {status?.provider === "chatgpt"
              ? status.ai.replaceAll("_", " ")
              : "Deployment API provider"}
          </h3>
          {status?.provider === "chatgpt" && (
            <>
              <p>Authorization: {status.login.state}</p>
              {status.login.url && (
                <p>
                  Open{" "}
                  <a
                    className="underline"
                    href={status.login.url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    ChatGPT verification
                  </a>{" "}
                  and enter <strong>{status.login.code}</strong>. This code
                  expires after 15 minutes.
                </p>
              )}
              <Button disabled={busy} onClick={() => void act("/chatgpt")}>
                {status.ai === "not_connected"
                  ? "Connect ChatGPT"
                  : "Reconnect ChatGPT"}
              </Button>{" "}
              <Button
                disabled={busy}
                onClick={() => void act("/chatgpt/cancel")}
              >
                Cancel authorization
              </Button>{" "}
              <Button
                disabled={busy}
                onClick={() => void act("/chatgpt", "DELETE")}
              >
                Disconnect ChatGPT
              </Button>
              <p>
                Disconnect removes local credentials only. Revoke provider
                access in your ChatGPT account. Failed reconnects retain
                existing credentials.
              </p>
            </>
          )}
          <Button disabled={busy} onClick={() => void act("/check-ai")}>
            Check AI compatibility
          </Button>
          <p>
            This explicit check sends a synthetic article and image to the
            configured provider and may use account quota.
          </p>
          <p>{status?.compatibility}</p>
        </div>
        <p role="status" className="mt-5 text-sm text-[var(--muted)]">
          {busy ? "Working…" : message}
        </p>
      </Modal>
      <Modal
        open={panel === "mail"}
        onOpenChange={(open) => {
          if (!open) openPanel(null);
        }}
        title="Kindle delivery"
        description="Configure your mail connection and Kindle recipient."
        wide
      >
        <form
          className="space-y-5 text-sm leading-6 text-[var(--muted)]"
          onSubmit={(e) => {
            e.preventDefault();
            void act("/mail", "PUT", {
              ...mail,
              password,
              managed: undefined,
              password_set: undefined,
            });
          }}
        >
          <p>
            {mail?.managed
              ? "Deployment-managed: update SMTP deployment variables and recreate Attic to change these fields."
              : "Settings are saved privately in the persistent state volume."}
          </p>
          <fieldset
            disabled={busy || mail?.managed}
            className="grid gap-3 sm:grid-cols-2"
          >
            {(
              [
                ["host", "SMTP host"],
                ["port", "SMTP port"],
                ["username", "SMTP username"],
                ["sender", "Sender email"],
                ["destination", "Kindle recipient"],
              ] as const
            ).map(([key, label]) => (
              <label key={key} className="grid gap-2 text-xs text-[var(--ink)]">
                {label}
                <input
                  className={input}
                  type={
                    key === "port"
                      ? "number"
                      : key === "sender" || key === "destination"
                        ? "email"
                        : "text"
                  }
                  value={mail?.[key] ?? ""}
                  onChange={(e) =>
                    setMail((m) =>
                      m
                        ? {
                            ...m,
                            [key]:
                              key === "port"
                                ? Number(e.target.value)
                                : e.target.value,
                          }
                        : m,
                    )
                  }
                  required={key !== "username"}
                />
              </label>
            ))}
            <label className="grid gap-2 text-xs text-[var(--ink)]">
              TLS mode
              <select
                className={input}
                value={mail?.tls_mode || "starttls"}
                onChange={(e) =>
                  setMail((m) => (m ? { ...m, tls_mode: e.target.value } : m))
                }
              >
                <option value="starttls">STARTTLS</option>
                <option value="implicit_tls">Implicit TLS</option>
                <option value="none">No TLS (local test relay only)</option>
              </select>
            </label>
            <label className="grid gap-2 text-xs text-[var(--ink)]">
              Password
              <input
                className={input}
                type="password"
                autoComplete="new-password"
                value={password}
                placeholder={
                  mail?.password_set
                    ? "Stored; blank retains password"
                    : "Not configured"
                }
                onChange={(e) => setPassword(e.target.value)}
              />
            </label>
            <label className="flex items-center gap-3 py-2 text-xs text-[var(--ink)] sm:col-span-2">
              <input
                type="checkbox"
                checked={mail?.enabled ?? false}
                onChange={(e) =>
                  setMail((m) => (m ? { ...m, enabled: e.target.checked } : m))
                }
              />{" "}
              Enable explicit delivery requests
            </label>
            <Button type="submit">Save mail settings</Button>
            <Button
              type="button"
              onClick={() =>
                void act("/mail", "PUT", {
                  ...mail,
                  password: "",
                  managed: undefined,
                  password_set: undefined,
                  clear_credentials: true,
                })
              }
            >
              Clear SMTP credentials
            </Button>
          </fieldset>
          <p>
            Approve the sender in Amazon’s Personal Document Email List. Your
            SMTP provider may require an app password. Configuration does not
            request delivery of saved items.
          </p>
          <div className="flex flex-col items-start gap-3 border-t border-[var(--line)] pt-5">
            <Button
              type="button"
              disabled={busy}
              onClick={() => void act("/mail/test")}
            >
              Test connection (no mail)
            </Button>
            <Button
              type="button"
              disabled={busy || !status?.mail.enabled}
              onClick={() =>
                void act("/mail/send-test", "POST", {
                  destination: status?.mail.destination,
                })
              }
            >
              Send test email to{" "}
              {status?.mail.destination || "unconfigured recipient"}
            </Button>
          </div>
          <p>
            To test delivery, explicitly use Send to Kindle on a saved PDF.
            Destination: {status?.mail.destination || "not configured"}. SMTP
            acceptance does not confirm device receipt.
          </p>
        </form>
        <p role="status" className="mt-5 text-sm text-[var(--muted)]">
          {busy ? "Working…" : message}
        </p>
      </Modal>
      <Modal
        open={panel === "runtime"}
        onOpenChange={(open) => {
          if (!open) openPanel(null);
        }}
        title="PDF and browser"
        description="Check actual page capture and PDF generation separately from process readiness."
      >
        <div>
          <Button disabled={busy} onClick={() => void act("/runtime")}>
            Check runtime
          </Button>
          {runtime && (
            <ul className="mt-5 space-y-3 text-sm leading-6 text-[var(--muted)]">
              {Object.entries(runtime).map(([k, v]) => (
                <li key={k} className="border-t border-[var(--line)] pt-3">
                  {k}: {v}
                </li>
              ))}
            </ul>
          )}
        </div>
        <p role="status" className="mt-5 text-sm text-[var(--muted)]">
          {busy ? "Checking…" : message}
        </p>
      </Modal>
      <Modal
        open={panel === "search"}
        onOpenChange={(open) => {
          if (!open) openPanel(null);
        }}
        title="Search index"
        description="Check applied updates or rebuild the index from your saved library."
      >
        <div className="space-y-4 text-sm leading-6 text-[var(--muted)]">
          <h3>
            Search ·{" "}
            {status?.search_configured ? "Configured" : "Not configured"}
          </h3>
          <Button disabled={busy} onClick={() => void act("/search", "GET")}>
            Check indexing progress
          </Button>{" "}
          <Button disabled={busy} onClick={() => void act("/search")}>
            Reindex saved items
          </Button>
          {search && (
            <p>
              {search.state}: {search.pending} pending, {search.failed} failed.
              Ready means updates were applied and a search query succeeded.
            </p>
          )}
        </div>
        <p role="status" className="mt-5 text-sm text-[var(--muted)]">
          {busy ? "Checking…" : message}
        </p>
      </Modal>
    </>
  );
}
