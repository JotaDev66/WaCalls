export type OnboardingStepKey = "link" | "call";

export type OnboardingStep = { key: OnboardingStepKey; done: boolean };

export const onboardingSteps = (
  hasPaired: boolean,
  firstCallDone: boolean,
): OnboardingStep[] => [
  { key: "link", done: hasPaired },
  { key: "call", done: firstCallDone },
];

export const onboardingComplete = (steps: OnboardingStep[]): boolean =>
  steps.every((s) => s.done);

export const currentStep = (
  steps: OnboardingStep[],
): OnboardingStepKey | null => steps.find((s) => !s.done)?.key ?? null;
