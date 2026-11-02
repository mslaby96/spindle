import { signalStrength } from "../format";

const bars = { weak: 1, ok: 2, strong: 3 } as const;
const radii = [5, 9.5, 14];

// An arc of the Wi-Fi fan, a quarter circle opening upwards around the dot.
function arc(r: number) {
  const d = r * Math.SQRT1_2;
  return `M${12 - d} ${19 - d}A${r} ${r} 0 0 1 ${12 + d} ${19 - d}`;
}

export function WifiSignal({ rssi }: { rssi: number }) {
  const strength = signalStrength(rssi);
  const lit = bars[strength];
  const label = `Wi-Fi signal: ${strength} (${lit} of ${radii.length})`;
  return (
    <svg className="wifi" viewBox="0 0 24 24" role="img" aria-label={label}>
      <title>{label}</title>
      <circle className="dot" cx="12" cy="19" r="1.9" />
      {radii.map((r, i) => (
        <path key={r} d={arc(r)} className={i < lit ? "on" : "off"} />
      ))}
    </svg>
  );
}
