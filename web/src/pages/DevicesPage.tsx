import { useEffect, useRef, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type Device } from "../api/client";
import { devicesKey, pairedStandKey } from "../api/events";
import { formatCountdown, formatDateTime, formatDbm, formatUptime, signalStrength } from "../format";

type Pairing = { code: string; expires_at: string };

// The Stands page polls for fresh diagnostics; going online or offline arrives as an event everywhere.
export function useDevices(refetchInterval?: number) {
  return useQuery({ queryKey: devicesKey, queryFn: () => api<Device[]>("/api/devices"), refetchInterval });
}

export function DevicesPage() {
  const queryClient = useQueryClient();
  const { data: devices } = useDevices(30_000);
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
          <StandRow key={d.id} device={d} onRemove={() => revoke.mutate(d.id)} />
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

function StandRow({ device: d, onRemove }: { device: Device; onRemove: () => void }) {
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [handedOver, setHandedOver] = useState(false);
  const wasPending = useRef(d.wifi_change_pending);
  const cancelled = useRef(false);
  const refresh = () => queryClient.invalidateQueries({ queryKey: devicesKey });
  const request = useMutation({
    mutationFn: () => api(`/api/devices/${d.id}/wifi-change`, { method: "POST" }),
    onSuccess: () => {
      cancelled.current = false;
      setConfirming(false);
      refresh();
    },
  });
  const cancel = useMutation({
    mutationFn: () => api(`/api/devices/${d.id}/wifi-change`, { method: "DELETE" }),
    onSuccess: refresh,
  });

  // A request that disappears without being cancelled was picked up by the stand.
  useEffect(() => {
    if (wasPending.current && !d.wifi_change_pending && !cancelled.current) setHandedOver(true);
    wasPending.current = d.wifi_change_pending;
  }, [d.wifi_change_pending]);

  const firmware = useFirmwareUpdate(d);
  const updatable = d.update_available && (!d.update || d.update.state === "failed");
  const error = request.error ?? cancel.error ?? firmware.request.error ?? firmware.cancel.error;
  return (
    <li className="card stand">
      <div className="row">
        <div>
          <p className="stand-name">
            <strong>{d.name}</strong>
            {d.online ? <span className="sticker calm">Online</span> : d.last_seen_at && <span className="sticker">Offline</span>}
          </p>
          <StandState device={d} />
          <Diagnostics device={d} />
        </div>
        <div className="stand-actions">
          {updatable && (
            <button className="secondary" onClick={() => firmware.request.mutate()} disabled={firmware.request.isPending}>
              {d.update?.state === "failed" ? "Try again" : "Update"}
            </button>
          )}
          {!d.wifi_change_pending && !confirming && (
            <button className="secondary" onClick={() => setConfirming(true)}>
              Change Wi-Fi
            </button>
          )}
          <button className="secondary" onClick={() => confirm(`Remove ${d.name}? Its token will stop working.`) && onRemove()}>
            Remove
          </button>
        </div>
      </div>
      {confirming && (
        <div className="confirm">
          <p>
            {d.name} will forget its Wi-Fi and open its own network, named Spindle- and four characters. Join it from your
            phone and open <code>http://spindle.setup</code> to pick the new network. It stays the same stand, history
            included.
          </p>
          <div className="choices">
            <button onClick={() => request.mutate()} disabled={request.isPending}>
              Forget Wi-Fi
            </button>
            <button className="secondary" onClick={() => setConfirming(false)}>
              Keep it
            </button>
          </div>
        </div>
      )}
      {d.wifi_change_pending && (
        <p className="pending">
          <span className="sticker">Wi-Fi change</span>
          <span>
            Waiting for {d.name} to pick this up
            {d.online ? "…" : ", it will when it comes back online."}
          </span>
          <button
            className="link"
            onClick={() => {
              cancelled.current = true;
              cancel.mutate();
            }}
            disabled={cancel.isPending}
          >
            Cancel
          </button>
        </p>
      )}
      {handedOver && (
        <div className="notice" role="status">
          <p>
            {d.name} is opening its setup network. On your phone join the Wi-Fi named Spindle- and four characters, then
            open <code>http://spindle.setup</code> and pick the new network.
          </p>
          <button className="secondary" onClick={() => setHandedOver(false)}>
            Done
          </button>
        </div>
      )}
      <UpdateStatus device={d} onCancel={() => firmware.cancel.mutate()} busy={firmware.cancel.isPending} />
      {firmware.done && (
        <p className="success" role="status">
          Updated: {d.name} is now on {firmware.done}.
        </p>
      )}
      {error && <p className="error">{error.message}</p>}
    </li>
  );
}

function useFirmwareUpdate(d: Device) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: devicesKey });
  const request = useMutation({
    mutationFn: () => api(`/api/devices/${d.id}/update`, { method: "POST" }),
    onSuccess: refresh,
  });
  const cancel = useMutation({
    mutationFn: () => api(`/api/devices/${d.id}/update`, { method: "DELETE" }),
    onSuccess: refresh,
  });
  const [done, setDone] = useState<string | null>(null);
  const was = useRef(d.update?.state);
  // An install that ends without a failure note means the stand came back on the new version.
  useEffect(() => {
    if (was.current === "installing" && !d.update && d.fw_version) setDone(d.fw_version);
    was.current = d.update?.state;
  }, [d.update, d.fw_version]);
  return { request, cancel, done };
}

function UpdateStatus({ device: d, onCancel, busy }: { device: Device; onCancel: () => void; busy: boolean }) {
  if (!d.update) return null;
  const { version, state } = d.update;
  if (state === "installing") {
    return (
      <p className="pending" role="status">
        <span className="sticker calm">Updating</span>
        <span>
          Installing {version}. {d.name} restarts and is back in about a minute.
        </span>
      </p>
    );
  }
  return (
    <p className="pending">
      <span className={state === "failed" ? "sticker" : "sticker calm"}>{state === "failed" ? "Update failed" : "Update"}</span>
      <span>
        {state === "failed"
          ? `${version} didn't take, ${d.name} is back on ${d.fw_version}.`
          : `${d.name} gets ${version} with its next report. It waits until no record is on the stand.`}
      </span>
      <button className="link" onClick={onCancel} disabled={busy}>
        {state === "failed" ? "Dismiss" : "Cancel"}
      </button>
    </p>
  );
}

function StandState({ device: d }: { device: Device }) {
  const state = d.online ? undefined : d.last_seen_at ? `Offline since ${formatDateTime(d.last_seen_at)}` : "Never connected";
  const line = [state, d.fw_version && `firmware ${d.fw_version}`, d.update_available && `${d.update_available} available`]
    .filter(Boolean)
    .join(" · ");
  return line ? <p className="muted">{line}</p> : null;
}

function Diagnostics({ device: d }: { device: Device }) {
  const parts = [
    d.rssi !== undefined && (
      <>
        Wi-Fi {signalStrength(d.rssi)} <span className="dbm">{formatDbm(d.rssi)}</span>
      </>
    ),
    d.reader_resets !== undefined && `${d.reader_resets} reader ${d.reader_resets === 1 ? "reset" : "resets"}`,
    d.uptime_sec !== undefined && `up ${formatUptime(d.uptime_sec)}`,
  ].filter(Boolean);
  if (parts.length === 0) return null;
  return (
    <p className="muted small diagnostics">
      {parts.map((p, i) => (
        <span key={i}>{p}</span>
      ))}
      {!d.online && <span>last known</span>}
    </p>
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
          characters. It has no password. Then open <code>http://spindle.setup</code> in Safari or any browser; you can switch
          to other apps, for example to copy your Wi-Fi password, and come back.
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
          <li>spindle.setup doesn't open: type <code>http://192.168.4.1</code> instead, and turn off mobile data for a moment if the phone keeps using it.</li>
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
