import { describe, it, expect } from "vitest";
import { detectLocale } from "./detect";

describe("detectLocale", () => {
  it("honours a valid stored locale over the browser language", () => {
    expect(detectLocale("pt-BR", "en-US")).toBe("pt-BR");
    expect(detectLocale("en", "pt-BR")).toBe("en");
  });

  it("falls back to the browser language when nothing is stored", () => {
    expect(detectLocale(null, "pt-BR")).toBe("pt-BR");
    expect(detectLocale(null, "en-US")).toBe("en");
  });

  it("matches Portuguese case-insensitively and by prefix", () => {
    expect(detectLocale(null, "PT-pt")).toBe("pt-BR");
    expect(detectLocale(null, "pt")).toBe("pt-BR");
  });

  it("ignores an invalid stored value", () => {
    expect(detectLocale("xx", "en-US")).toBe("en");
    expect(detectLocale("", "pt-BR")).toBe("pt-BR");
  });
});
