import { describe, it, expect } from "vitest";
import {
  filterContacts,
  contactInitials,
  avatarColorIndex,
  hasLetters,
} from "./contacts";
import type { Contact } from "@/types/contact";

const sample: Contact[] = [
  { jid: "a@s.whatsapp.net", name: "Alice Silva", phone: "5511999990000" },
  { jid: "b@s.whatsapp.net", name: "Bruno", phone: "5511888887777" },
];

describe("filterContacts", () => {
  it("returns all on empty query", () => {
    expect(filterContacts("", sample)).toHaveLength(2);
  });
  it("matches by name case-insensitively", () => {
    expect(filterContacts("alice", sample).map((c) => c.name)).toEqual([
      "Alice Silva",
    ]);
  });
  it("matches by phone digits", () => {
    expect(filterContacts("8888", sample).map((c) => c.name)).toEqual([
      "Bruno",
    ]);
  });
  it("returns empty when nothing matches", () => {
    expect(filterContacts("zzz", sample)).toEqual([]);
  });
});

describe("contactInitials", () => {
  it("takes up to two words", () => {
    expect(contactInitials("Alice Silva")).toBe("AS");
  });
  it("single word yields one letter", () => {
    expect(contactInitials("Bruno")).toBe("B");
  });
  it("empty yields #", () => {
    expect(contactInitials("")).toBe("#");
  });
});

describe("avatarColorIndex", () => {
  it("is deterministic and bounded", () => {
    const a = avatarColorIndex("Alice");
    expect(a).toBe(avatarColorIndex("Alice"));
    expect(a).toBeGreaterThanOrEqual(0);
    expect(a).toBeLessThan(6);
  });
});

describe("hasLetters", () => {
  it("true for names", () => {
    expect(hasLetters("Alice")).toBe(true);
  });
  it("false for a pure phone number", () => {
    expect(hasLetters("558799657022")).toBe(false);
  });
  it("false for empty", () => {
    expect(hasLetters("")).toBe(false);
  });
});
