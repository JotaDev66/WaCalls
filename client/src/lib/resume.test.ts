import { describe, it, expect } from "vitest";
import { needsAudioResume } from "./resume";

describe("needsAudioResume", () => {
  it("is true for an established call with no local connection", () => {
    expect(needsAudioResume("connected", false)).toBe(true);
    expect(needsAudioResume("reconnecting", false)).toBe(true);
  });

  it("is false once a local connection exists", () => {
    expect(needsAudioResume("connected", true)).toBe(false);
  });

  it("does not resume a call still in setup", () => {
    expect(needsAudioResume("ringing", false)).toBe(false);
    expect(needsAudioResume("starting", false)).toBe(false);
  });
});
