import type { CSSProperties } from "react";

const dyes = ["var(--tomato)", "var(--teal)", "var(--cobalt)", "var(--plum)"];

// Ink for every named dye; mustard is the only light one.
const namedInk: Record<string, string> = {
  "var(--mustard)": "var(--mustard-ink)",
  "var(--tomato)": "#fff",
  "var(--teal)": "#fff",
  "var(--cobalt)": "#fff",
  "var(--plum)": "#fff",
};

const white = "#ffffff";
const binBlack = "#191714";

export function dye(key: string) {
  let h = 0;
  for (const c of key) h = (h * 31 + c.charCodeAt(0)) | 0;
  return dyes[Math.abs(h) % dyes.length];
}

const toLinear = (v: number) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
const fromLinear = (v: number) => (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055);

function channels(hex: string) {
  const n = parseInt(hex.slice(1), 16);
  return [n >> 16, (n >> 8) & 255, n & 255];
}

function luminance(hex: string) {
  const [r, g, b] = channels(hex).map((v) => toLinear(v / 255));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a: string, b: string) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function fromOklch(L: number, C: number, hue: number) {
  const a = C * Math.cos(hue);
  const b = C * Math.sin(hue);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  return (
    "#" +
    [
      4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
      -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
      -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
    ]
      .map((v) => Math.round(Math.min(Math.max(fromLinear(v), 0), 1) * 255).toString(16).padStart(2, "0"))
      .join("")
  );
}

const inkFor = (color: string) => (contrast(color, white) >= contrast(color, binBlack) ? white : binBlack);

// The server picks the cover colour; here it only gets dye strength and a lightness its tab caps stay legible on,
// so the hue Home Assistant gets and the hue on screen are the same.
export function coverDye(hex: string) {
  const [r, g, b] = channels(hex).map((v) => toLinear(v / 255));
  const l = Math.cbrt(0.4122214708 * r + 0.5363026619 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  const hue = Math.atan2(B, A);
  const chroma = Math.max(Math.hypot(A, B), 0.09);
  let lightness = Math.min(Math.max(L, 0.4), 0.55);
  let color = fromOklch(lightness, chroma, hue);
  while (contrast(color, inkFor(color)) < 4.5 && lightness > 0.3) {
    lightness -= 0.01;
    color = fromOklch(lightness, chroma, hue);
  }
  return { color, ink: inkFor(color) };
}

export function tabStyle(color?: string): CSSProperties | undefined {
  if (!color) return undefined;
  const ink = namedInk[color];
  if (ink) return { "--tab": color, "--tab-ink": ink } as CSSProperties;
  if (!/^#[0-9a-f]{6}$/i.test(color)) return undefined;
  const d = coverDye(color);
  return { "--tab": d.color, "--tab-ink": d.ink } as CSSProperties;
}
