import { describe, expect, it } from "vitest";
import { micErrorKind, micErrorMessage } from "./mic-error";

const named = (name: string) => Object.assign(new Error("boom"), { name });

describe("micErrorKind", () => {
  it("classifies device-busy errors", () => {
    expect(micErrorKind(named("NotReadableError"))).toBe("busy");
    expect(micErrorKind(named("AbortError"))).toBe("busy");
  });
  it("classifies permission errors", () => {
    expect(micErrorKind(named("NotAllowedError"))).toBe("denied");
    expect(micErrorKind(named("SecurityError"))).toBe("denied");
  });
  it("classifies missing-device errors", () => {
    expect(micErrorKind(named("NotFoundError"))).toBe("notfound");
    expect(micErrorKind(named("OverconstrainedError"))).toBe("notfound");
  });
  it("passes through everything else", () => {
    expect(micErrorKind(new Error("api 500"))).toBeNull();
    expect(micErrorKind(null)).toBeNull();
    expect(micErrorKind("string")).toBeNull();
  });
});

describe("micErrorMessage", () => {
  const m = { micBusy: "B", micDenied: "D", micNotFound: "N" };
  it("maps kinds to catalog messages and null otherwise", () => {
    expect(micErrorMessage(named("NotReadableError"), m)).toBe("B");
    expect(micErrorMessage(named("NotAllowedError"), m)).toBe("D");
    expect(micErrorMessage(named("NotFoundError"), m)).toBe("N");
    expect(micErrorMessage(new Error("x"), m)).toBeNull();
  });
});
