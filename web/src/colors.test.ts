import { contrast, coverDye, dye, tabStyle } from "./colors";

describe("dye", () => {
  it("gives the same divider colour for the same key", () => {
    expect(dye("K")).toBe(dye("K"));
  });

  it("spreads keys over the divider dyes", () => {
    const used = new Set("ABCDEFGHIJKLMNOPQRSTUVWXYZ".split("").map(dye));
    expect(used).toEqual(new Set(["var(--tomato)", "var(--teal)", "var(--cobalt)", "var(--plum)"]));
  });
});

describe("coverDye", () => {
  it("turns a washed-out cover colour into a saturated dye with legible caps", () => {
    const d = coverDye("#d8d9e8");
    expect(d.color).not.toBe("#d8d9e8");
    expect(contrast(d.color, d.ink)).toBeGreaterThanOrEqual(4.5);
  });

  it("keeps every cover colour legible under its tab ink", () => {
    for (const hex of ["#ffffff", "#000000", "#ca9876", "#081b24", "#693845", "#f0c800", "#20c060"]) {
      const d = coverDye(hex);
      expect(contrast(d.color, d.ink), hex).toBeGreaterThanOrEqual(4.5);
    }
  });

  it("keeps the hue of a colourful cover", () => {
    const d = coverDye("#693845");
    const [r, g, b] = [1, 3, 5].map((i) => parseInt(d.color.slice(i, i + 2), 16));
    expect(r).toBeGreaterThan(g);
    expect(r).toBeGreaterThan(b);
  });
});

describe("tabStyle", () => {
  it("leaves the default tab colour alone when there is no colour", () => {
    expect(tabStyle(undefined)).toBeUndefined();
  });

  it("pairs a named dye with its ink", () => {
    expect(tabStyle("var(--mustard)")).toEqual({ "--tab": "var(--mustard)", "--tab-ink": "var(--mustard-ink)" });
    expect(tabStyle("var(--teal)")).toEqual({ "--tab": "var(--teal)", "--tab-ink": "#fff" });
  });

  it("dyes a cover colour", () => {
    const d = coverDye("#d8d9e8");
    expect(tabStyle("#d8d9e8")).toEqual({ "--tab": d.color, "--tab-ink": d.ink });
  });
});
