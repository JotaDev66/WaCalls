import { create } from "zustand";
import { en } from "@/i18n/en";
import { ptBR } from "@/i18n/pt-BR";
import { detectLocale, type Locale } from "@/i18n/detect";
import type { Messages } from "@/i18n/messages";

const catalog: Record<Locale, Messages> = { en, "pt-BR": ptBR };

const initial = detectLocale(
  localStorage.getItem("locale"),
  navigator.language,
);

type State = {
  locale: Locale;
  messages: Messages;
  setLocale: (locale: Locale) => void;
  toggle: () => void;
};

export const useLocale = create<State>((set) => ({
  locale: initial,
  messages: catalog[initial],
  setLocale: (locale) => {
    localStorage.setItem("locale", locale);
    set({ locale, messages: catalog[locale] });
  },
  toggle: () =>
    set((s) => {
      const next: Locale = s.locale === "en" ? "pt-BR" : "en";
      localStorage.setItem("locale", next);
      return { locale: next, messages: catalog[next] };
    }),
}));

useLocale.subscribe((s) => {
  document.documentElement.lang = s.locale;
});
document.documentElement.lang = useLocale.getState().locale;
