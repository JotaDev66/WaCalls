import { useLocale } from "@/stores/locale";

export const useT = () => useLocale((s) => s.messages);
