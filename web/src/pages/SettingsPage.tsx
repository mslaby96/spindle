import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router";

import { api } from "../api/client";
import { DiscogsImport } from "../components/DiscogsImport";

export function SettingsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);

  async function changePassword(e: FormEvent) {
    e.preventDefault();
    try {
      await api("/api/account/password", { body: { current, new: next } });
      setCurrent("");
      setNext("");
      setMessage({ ok: true, text: "Password changed. Other devices were signed out." });
    } catch (err) {
      setMessage({ ok: false, text: err instanceof Error ? err.message : String(err) });
    }
  }

  async function logout() {
    await api("/api/logout", { method: "POST" });
    queryClient.clear();
    navigate("/login", { replace: true });
  }

  return (
    <section>
      <h1>Account</h1>
      <form className="card narrow" onSubmit={changePassword}>
        <h2>Change password</h2>
        <label>
          Current password
          <input
            type="password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        <label>
          New password
          <input
            type="password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
            autoComplete="new-password"
            minLength={8}
            required
          />
        </label>
        {message && <p className={message.ok ? "success" : "error"}>{message.text}</p>}
        <button>Change password</button>
      </form>
      <div className="card narrow">
        <h2>Import from Discogs</h2>
        <DiscogsImport />
      </div>
      <div className="card narrow">
        <h2>Your data</h2>
        <p className="muted">
          Download your collection and listening history to keep or take elsewhere. Backups of the whole server are
          made by Spindle itself; see its README.
        </p>
        <div className="row">
          <a className="button secondary" href="/api/export.json" download>
            Everything (JSON)
          </a>
          <a className="button secondary" href="/api/export.csv" download>
            Diary (CSV)
          </a>
        </div>
      </div>
      <button className="secondary" onClick={logout}>
        Sign out
      </button>
    </section>
  );
}
