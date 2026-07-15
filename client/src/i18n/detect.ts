export type Locale = "en" | "pt-BR";

export const detectLocale = (
  stored: string | null,
  navLang: string,
): Locale => {
  if (stored === "en" || stored === "pt-BR") return stored;
  return navLang.toLowerCase().startsWith("pt") ? "pt-BR" : "en";
};
