import { describe, it, expect, vi, afterEach } from "vitest";
import { formatCallDuration } from "./format";

describe("formatCallDuration", () => {
  afterEach(() => vi.useRealTimers());

  it("returns the status verbatim when not connected", () => {
    expect(formatCallDuration(0, "ringing")).toBe("ringing");
    expect(formatCallDuration(0, "starting")).toBe("starting");
    expect(formatCallDuration(0, "reconnecting")).toBe("reconnecting");
    expect(formatCallDuration(0, "ended")).toBe("ended");
  });

  it("formats elapsed time as MM:SS when connected", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 1, 0, 0, 0));
    expect(formatCallDuration(Date.now() - 75_000, "connected")).toBe("01:15");
  });

  it("zero-pads minutes and seconds", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 1, 0, 0, 0));
    expect(formatCallDuration(Date.now() - 5_000, "connected")).toBe("00:05");
  });
});
