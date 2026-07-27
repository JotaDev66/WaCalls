import { describe, expect, it } from "vitest";
import { initials } from "./initials";

describe("initials", () => {
  it("takes the first letter of the first two words", () => {
    expect(initials("Vendas SP")).toBe("VS");
  });
  it("takes two chars of a single word", () => {
    expect(initials("WhatsApp")).toBe("WH");
  });
  it("ignores extra whitespace", () => {
    expect(initials("  suporte   noturno ")).toBe("SN");
  });
  it("falls back for empty names", () => {
    expect(initials("   ")).toBe("?");
  });
});
