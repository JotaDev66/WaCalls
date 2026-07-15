import { describe, it, expect } from "vitest";
import { gateFor } from "./auth-mode";

describe("gateFor", () => {
  it("open -> none", () => {
    expect(gateFor({ mode: "open", authenticated: true })).toBe("none");
  });
  it("login unauthenticated -> login", () => {
    expect(gateFor({ mode: "login", authenticated: false })).toBe("login");
  });
  it("login authenticated -> none", () => {
    expect(gateFor({ mode: "login", authenticated: true })).toBe("none");
  });
  it("token unauthenticated -> token", () => {
    expect(gateFor({ mode: "token", authenticated: false })).toBe("token");
  });
});
