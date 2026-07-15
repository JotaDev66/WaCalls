import { create } from "zustand";

type State = {
  firstCallDone: boolean;
  dismissed: boolean;
  markFirstCall: () => void;
  dismiss: () => void;
};

export const useOnboarding = create<State>((set) => ({
  firstCallDone: localStorage.getItem("onboarding-first-call") === "1",
  dismissed: localStorage.getItem("onboarding-dismissed") === "1",
  markFirstCall: () =>
    set((s) => {
      if (s.firstCallDone) return s;
      localStorage.setItem("onboarding-first-call", "1");
      return { firstCallDone: true };
    }),
  dismiss: () => {
    localStorage.setItem("onboarding-dismissed", "1");
    set({ dismissed: true });
  },
}));
