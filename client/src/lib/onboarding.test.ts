import { describe, it, expect } from "vitest";
import { onboardingSteps, onboardingComplete, currentStep } from "./onboarding";

describe("onboardingSteps", () => {
  it("marks each step done from its boolean signal", () => {
    const steps = onboardingSteps(true, false);
    expect(steps.map((s) => s.done)).toEqual([true, false]);
    expect(steps.map((s) => s.key)).toEqual(["link", "call"]);
  });

  it("currentStep is the first not-done step, null when all done", () => {
    expect(currentStep(onboardingSteps(false, false))).toBe("link");
    expect(currentStep(onboardingSteps(true, false))).toBe("call");
    expect(currentStep(onboardingSteps(true, true))).toBeNull();
  });

  it("onboardingComplete is true only when every step is done", () => {
    expect(onboardingComplete(onboardingSteps(true, true))).toBe(true);
    expect(onboardingComplete(onboardingSteps(true, false))).toBe(false);
    expect(onboardingComplete(onboardingSteps(false, false))).toBe(false);
  });
});
