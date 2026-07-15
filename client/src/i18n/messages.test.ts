import { describe, it, expect } from "vitest";
import { en } from "./en";
import { ptBR } from "./pt-BR";

const shapeErrors = (a: unknown, b: unknown, path: string): string[] => {
  if (typeof a !== typeof b) {
    return [`${path}: ${typeof a} vs ${typeof b}`];
  }
  if (typeof a === "object" && a !== null && b !== null) {
    const ak = Object.keys(a as object).sort();
    const bk = Object.keys(b as object).sort();
    const errs: string[] = [];
    if (ak.join(",") !== bk.join(",")) {
      errs.push(`${path}: keys [${ak}] vs [${bk}]`);
    }
    for (const k of ak) {
      if (bk.includes(k)) {
        errs.push(
          ...shapeErrors(
            (a as Record<string, unknown>)[k],
            (b as Record<string, unknown>)[k],
            `${path}.${k}`,
          ),
        );
      }
    }
    return errs;
  }
  return [];
};

describe("message catalogs", () => {
  it("en and pt-BR share the exact same keys and value kinds", () => {
    expect(shapeErrors(en, ptBR, "")).toEqual([]);
  });
});
