import { describe, it, expect } from "vitest";
import {
  callStatusTone,
  sessionStateTone,
  callStatusPulse,
  sessionStatePulse,
} from "./status";

describe("status tones", () => {
  it("maps call statuses to tones", () => {
    expect(callStatusTone("connected")).toBe("ok");
    expect(callStatusTone("ringing")).toBe("neutral");
    expect(callStatusTone("starting")).toBe("neutral");
    expect(callStatusTone("reconnecting")).toBe("warn");
    expect(callStatusTone("ended")).toBe("neutral");
  });

  it("maps session states to tones", () => {
    expect(sessionStateTone("open")).toBe("ok");
    expect(sessionStateTone("qr")).toBe("neutral");
    expect(sessionStateTone("connecting")).toBe("neutral");
    expect(sessionStateTone("logged_out")).toBe("danger");
  });

  it("pulses only the live states", () => {
    expect(callStatusPulse("connected")).toBe(true);
    expect(callStatusPulse("ringing")).toBe(false);
    expect(callStatusPulse("reconnecting")).toBe(false);
    expect(sessionStatePulse("open")).toBe(true);
    expect(sessionStatePulse("connecting")).toBe(false);
    expect(sessionStatePulse("logged_out")).toBe(false);
  });
});
