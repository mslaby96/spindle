import { describe, expect, it } from "vitest";

import { formatElapsed, formatLength, formatUptime, signalStrength } from "./format";

describe("formatLength", () => {
  it("rounds to whole minutes before splitting off hours", () => {
    expect(formatLength(2578)).toBe("43 min");
    expect(formatLength(3599)).toBe("1 h 0 min");
    expect(formatLength(7170)).toBe("2 h 0 min");
    expect(formatLength(5400)).toBe("1 h 30 min");
    expect(formatLength(0)).toBe("0 min");
  });
});

describe("formatElapsed", () => {
  it("shows minutes and seconds under an hour", () => {
    expect(formatElapsed(0)).toBe("0:00");
    expect(formatElapsed(23 * 60_000 + 42_000)).toBe("23:42");
    expect(formatElapsed(59 * 60_000 + 59_999)).toBe("59:59");
  });

  it("adds hours from one hour on", () => {
    expect(formatElapsed(3_600_000)).toBe("1:00:00");
    expect(formatElapsed(3_600_000 + 5 * 60_000 + 7_000)).toBe("1:05:07");
  });

  it("never goes below zero", () => {
    expect(formatElapsed(-5_000)).toBe("0:00");
  });
});

describe("signalStrength", () => {
  it("calls -67 dBm and stronger strong", () => {
    expect(signalStrength(-40)).toBe("strong");
    expect(signalStrength(-67)).toBe("strong");
  });

  it("calls -68 to -80 dBm ok", () => {
    expect(signalStrength(-68)).toBe("ok");
    expect(signalStrength(-80)).toBe("ok");
  });

  it("calls anything below -80 dBm weak", () => {
    expect(signalStrength(-81)).toBe("weak");
  });
});

describe("formatUptime", () => {
  it("uses minutes and hours for the first two days", () => {
    expect(formatUptime(30)).toBe("0 min");
    expect(formatUptime(12 * 60)).toBe("12 min");
    expect(formatUptime(3 * 3600 + 12 * 60)).toBe("3 h 12 min");
    expect(formatUptime(48 * 3600 - 60)).toBe("47 h 59 min");
  });

  it("switches to days after that", () => {
    expect(formatUptime(48 * 3600)).toBe("2 days");
    expect(formatUptime(10 * 86_400 + 5 * 3600)).toBe("10 days");
  });
});
