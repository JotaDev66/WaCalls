import { describe, it, expect } from "vitest";
import { looksLikePhone, omniboxItems } from "./omnibox";

describe("looksLikePhone", () => {
  it("accepts phone-like strings", () => {
    expect(looksLikePhone("5511999999999")).toBe(true);
    expect(looksLikePhone("+55 11 99999-9999")).toBe(true);
    expect(looksLikePhone("(11) 3456")).toBe(true);
  });

  it("rejects text and too-short digits", () => {
    expect(looksLikePhone("whatsapp")).toBe(false);
    expect(looksLikePhone("")).toBe(false);
    expect(looksLikePhone("12")).toBe(false);
    expect(looksLikePhone("a123456")).toBe(false);
  });
});

describe("omniboxItems", () => {
  const sessions = [
    { id: "s1", name: "Support" },
    { id: "s2", name: "Sales" },
  ];

  it("returns a single dial item for a phone query when dialing is possible", () => {
    expect(omniboxItems("5511999999999", sessions, true)).toEqual([
      { kind: "dial", phone: "5511999999999" },
    ]);
  });

  it("falls back to sessions when dialing is not possible", () => {
    const items = omniboxItems("5511999999999", sessions, false);
    expect(items).toEqual([{ kind: "new-session" }]);
  });

  it("filters sessions by name and always offers new-session", () => {
    expect(omniboxItems("sup", sessions, true)).toEqual([
      { kind: "session", id: "s1", name: "Support" },
      { kind: "new-session" },
    ]);
  });

  it("lists all sessions for an empty query", () => {
    const items = omniboxItems("", sessions, true);
    expect(items.map((i) => i.kind)).toEqual([
      "session",
      "session",
      "new-session",
    ]);
  });
});
