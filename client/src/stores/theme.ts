import { create } from "zustand";

type Theme = "light" | "dark";
const initial: Theme =
  (localStorage.getItem("theme") as Theme | null) ??
  (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");

export const useTheme = create<{ theme: Theme; toggle: () => void }>((set) => ({
  theme: initial,
  toggle: () =>
    set((s) => {
      const next: Theme = s.theme === "dark" ? "light" : "dark";
      localStorage.setItem("theme", next);
      return { theme: next };
    }),
}));

useTheme.subscribe((s) => {
  document.documentElement.classList.toggle("dark", s.theme === "dark");
});
document.documentElement.classList.toggle(
  "dark",
  useTheme.getState().theme === "dark",
);
