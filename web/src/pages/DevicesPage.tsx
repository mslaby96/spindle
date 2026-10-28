import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type Device } from "../api/client";
import { formatFull } from "../format";

export const devicesKey = ["devices"] as const;

export function useDevices() {
  return useQuery({ queryKey: devicesKey, queryFn: () => api<Device[]>("/api/devices") });
}

export function DevicesPage() {
  const queryClient = useQueryClient();
  const { data: devices } = useDevices();
  const [name, setName] = useState("");
  const [token, setToken] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: (name: string) => api<{ device: Device; token: string }>("/api/devices", { body: { name } }),
    onSuccess: (res) => {
      setToken(res.token);
      setName("");
      queryClient.invalidateQueries({ queryKey: devicesKey });
    },
  });
  const revoke = useMutation({
    mutationFn: (id: number) => api(`/api/devices/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: devicesKey }),
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    create.mutate(name);
  }

  return (
    <section>
      <h1>Stands</h1>
      {token && (
        <div className="card notice" role="dialog" aria-label="Stand token">
          <p>Copy this token into the stand's config.json. It will not be shown again.</p>
          <code className="token">{token}</code>
          <button onClick={() => setToken(null)}>Done</button>
        </div>
      )}
      <ul className="list">
        {devices?.map((d) => (
          <li key={d.id} className="card row">
            <div>
              <strong>{d.name}</strong>
              <p className="muted">
                {d.last_seen_at ? `Last seen ${formatFull(d.last_seen_at)}` : "Never connected"}
                {d.fw_version && ` · firmware ${d.fw_version}`}
              </p>
            </div>
            <button
              className="secondary"
              onClick={() => confirm(`Remove ${d.name}? Its token will stop working.`) && revoke.mutate(d.id)}
            >
              Remove
            </button>
          </li>
        ))}
      </ul>
      <form className="card row" onSubmit={submit}>
        <input placeholder="Stand name, e.g. Living room" value={name} onChange={(e) => setName(e.target.value)} required />
        <button disabled={create.isPending}>Add stand</button>
      </form>
      {create.error && <p className="error">{create.error.message}</p>}
    </section>
  );
}
