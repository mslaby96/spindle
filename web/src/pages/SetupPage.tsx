import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router";

import { api, ApiError, setupKey } from "../api/client";

export function SetupPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [code, setCode] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/api/setup", { body: { code, username, password } });
      queryClient.setQueryData(setupKey, { needed: false });
      navigate("/", { replace: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        navigate("/login", { replace: true });
        return;
      }
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card narrow" onSubmit={submit}>
      <h1>Welcome to Spindle</h1>
      <p className="muted">
        Create your account. The setup code is printed in the server log when Spindle starts for the first time.
      </p>
      <label>
        Setup code
        <input value={code} onChange={(e) => setCode(e.target.value)} autoComplete="off" required />
      </label>
      <label>
        Username
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />
      </label>
      <label>
        Password
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          minLength={8}
          required
        />
      </label>
      {error && <p className="error">{error}</p>}
      <button disabled={busy}>Create account</button>
    </form>
  );
}
