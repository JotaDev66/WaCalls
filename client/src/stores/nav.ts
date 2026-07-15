import { create } from "zustand";

type View = "console" | "contacts";

type NavState = {
  view: View;
  setView: (v: View) => void;
};

export const useNav = create<NavState>((set) => ({
  view: "console",
  setView: (view) => set({ view }),
}));
