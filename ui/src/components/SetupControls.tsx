import { useEffect, useState } from "react";
import { useArchive } from "../state";
import { Button, input } from "./primitives";
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
    <section className="mt-8 space-y-6 border border-[var(--line)] bg-[var(--paper)] p-6">
      <h2 className="font-display text-2xl">Setup and recovery</h2>
      <p>
        Process readiness checks the database and storage. Check each feature
        below separately.
      </p>
      <div className="space-y-3">
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
            <Button disabled={busy} onClick={() => void act("/chatgpt/cancel")}>
              Cancel authorization
            </Button>{" "}
            <Button
              disabled={busy}
              onClick={() => void act("/chatgpt", "DELETE")}
            >
              Disconnect ChatGPT
            </Button>
            <p>
              Disconnect removes local credentials only. Revoke provider access
              in your ChatGPT account. Failed reconnects retain existing
              credentials.
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
      <form
        className="space-y-3"
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
        <h3>SMTP and Kindle</h3>
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
            <label key={key}>
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
          <label>
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
          <label>
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
          <label>
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
          Approve the sender in Amazon’s Personal Document Email List. Your SMTP
          provider may require an app password. Configuration does not request
          delivery of saved items.
        </p>
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
        <p>
          To test delivery, explicitly use Send to Kindle on a saved PDF.
          Destination: {status?.mail.destination || "not configured"}. SMTP
          acceptance does not confirm device receipt.
        </p>
      </form>
      <div>
        <h3>PDF and browser</h3>
        <Button disabled={busy} onClick={() => void act("/runtime")}>
          Check runtime
        </Button>
        {runtime && (
          <ul>
            {Object.entries(runtime).map(([k, v]) => (
              <li key={k}>
                {k}: {v}
              </li>
            ))}
          </ul>
        )}
      </div>
      <div>
        <h3>
          Search · {status?.search_configured ? "Configured" : "Not configured"}
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
      <p role="status">{busy ? "Checking…" : message}</p>
    </section>
  );
}
