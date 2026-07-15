import { describe, it, expect } from "vitest";
import { waveLevel } from "./waveform";

describe("waveLevel", () => {
  it("maps silence to the floor and full level to 1", () => {
    expect(waveLevel(-60)).toBe(0.25);
    expect(waveLevel(0)).toBe(1);
  });

  it("scales linearly between the floor and 1", () => {
    expect(waveLevel(-30)).toBeCloseTo(0.625, 5);
  });

  it("clamps out-of-range dB", () => {
    expect(waveLevel(-120)).toBe(0.25);
    expect(waveLevel(20)).toBe(1);
  });

  it("honours a custom floor", () => {
    expect(waveLevel(-60, 0.1)).toBe(0.1);
    expect(waveLevel(0, 0.1)).toBe(1);
  });
});
