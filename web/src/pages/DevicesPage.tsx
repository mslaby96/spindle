import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type Device } from "../api/client";
import { devicesKey, pairedStandKey } from "../api/events";
import { formatCountdown, formatFull } from "../format";

type Pairing = { code: string; expires_at: string };

export function useDevices() {
  return useQuery({ queryKey: devicesKey, queryFn: () => api<Device[]>("/api/devices") });
}

export function DevicesPage() {
  const queryClient = useQueryClient();
  const { data: devices } = useDevices();
  const [name, setName] = useState("");
  const [pairing, setPairing] = useState<Pairing | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const paired = useQuery<Device | null>({ queryKey: pairedStandKey, queryFn: () => null, staleTime: Infinity });

  const startPairing = useMutation({
    mutationFn: (name: string) => api<Pairing>("/api/devices/pairing", { body: { name } }),
    onSuccess: (p) => {
      queryClient.setQueryData(pairedStandKey, null);
      setPairing(p);
    },
  });
  const createToken = useMutation({
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

  const connected = pairing && paired.data ? paired.data : null;

  function submit(e: FormEvent) {
    e.preventDefault();
    startPairing.mutate(name);
  }

  function finish() {
    setPairing(null);
    setName("");
    queryClient.setQueryData(pairedStandKey, null);
  }

  return (
    <section>
      <h1>Stands</h1>
      {connected && (
        <div className="card notice" role="status">
          <p>{connected.name} is connected.</p>
          <button onClick={finish}>Done</button>
        </div>
      )}
      {pairing && !connected && (
        <PairingCard pairing={pairing} onRenew={() => startPairing.mutate(name)} onCancel={finish} busy={startPairing.isPending} />
      )}
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
      {!pairing && (
        <form className="card" onSubmit={submit}>
          <p className="muted">
            Name the stand and press Add stand. You get a short code, then set it up from your phone: the stand opens
            its own Wi-Fi network with a page where you pick your home Wi-Fi and type the code.
          </p>
          <div className="row">
            <input placeholder="Stand name, e.g. Living room" value={name} onChange={(e) => setName(e.target.value)} required />
            <button disabled={startPairing.isPending}>Add stand</button>
          </div>
          <button type="button" className="link" disabled={!name.trim()} onClick={() => createToken.mutate(name)}>
            Set up over USB instead
          </button>
        </form>
      )}
      {(startPairing.error ?? createToken.error) && <p className="error">{(startPairing.error ?? createToken.error)?.message}</p>}
    </section>
  );
}

function PairingCard({ pairing, onRenew, onCancel, busy }: { pairing: Pairing; onRenew: () => void; onCancel: () => void; busy: boolean }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const left = new Date(pairing.expires_at).getTime() - now;
  const port = window.location.port || (window.location.protocol === "https:" ? "443" : "80");

  return (
    <div className="card notice" role="dialog" aria-label="Pair a stand">
      <ol className="steps">
        <li>
          Plug the stand in. A new stand, or one you reset, waits to be set up: its light stays on with a short break every
          two seconds.
        </li>
        <li>
          On your phone, open the Wi-Fi settings and join the network named <strong>Spindle-</strong> followed by four
          characters. It has no password. A setup page opens by itself; if it doesn't, open <code>http://192.168.4.1</code>{" "}
          in the browser.
        </li>
        <li>
          On that page pick your home Wi-Fi and type its password.
        </li>
        <li>
          {isLocal(window.location.hostname) ? (
            <>
              As the server address, type this computer's address on your home network with port {port}, e.g.{" "}
              <code>http://192.168.1.10:{port}</code>. You opened Spindle as {window.location.hostname}, which only works on
              this computer. On a Mac the address is under System Settings → Wi-Fi → Details; on Windows run{" "}
              <code>ipconfig</code>.
            </>
          ) : (
            <>
              As the server address, type <code>{window.location.origin}</code>
            </>
          )}
        </li>
        <li>Enter this code and press Connect:</li>
      </ol>
      <code className="token pair-code">{pairing.code}</code>
      {left > 0 ? (
        <p className="muted">Code expires in {formatCountdown(left)}. This page updates when the stand connects.</p>
      ) : (
        <p className="muted">This code has run out.</p>
      )}
      <details>
        <summary>Something not working?</summary>
        <ul>
          <li>No Spindle network: the stand already has Wi-Fi settings. Hold the BOOTSEL button on the Pico for five seconds to clear them.</li>
          <li>Your phone drops the Spindle network while connecting: join it again, the page shows how it went.</li>
          <li>The page says the server can't be reached: check that the stand's Wi-Fi is the same network this server is on.</li>
        </ul>
      </details>
      <div className="row">
        {left <= 0 && (
          <button onClick={onRenew} disabled={busy}>
            New code
          </button>
        )}
        <button className="secondary" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}

function isLocal(hostname: string) {
  return hostname === "localhost" || hostname.startsWith("127.") || hostname === "[::1]" || hostname === "::1";
}
