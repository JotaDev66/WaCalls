import { describe, it, expect, vi, afterEach } from "vitest";
import { formatCallDuration } from "./format";

describe("formatCallDuration", () => {
  afterEach(() => vi.useRealTimers());

  it("formats elapsed time as MM:SS", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 1, 0, 0, 0));
    expect(formatCallDuration(Date.now() - 75_000)).toBe("01:15");
  });

  it("zero-pads minutes and seconds", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 1, 0, 0, 0));
    expect(formatCallDuration(Date.now() - 5_000)).toBe("00:05");
  });
});
