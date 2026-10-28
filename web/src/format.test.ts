import { describe, expect, it } from "vitest";

import { formatLength } from "./format";

describe("formatLength", () => {
  it("rounds to whole minutes before splitting off hours", () => {
    expect(formatLength(2578)).toBe("43 min");
    expect(formatLength(3599)).toBe("1 h 0 min");
    expect(formatLength(7170)).toBe("2 h 0 min");
    expect(formatLength(5400)).toBe("1 h 30 min");
    expect(formatLength(0)).toBe("0 min");
  });
});
