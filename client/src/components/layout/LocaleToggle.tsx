import { Button } from "@/components/ui/button";
import { useLocale } from "@/stores/locale";
import { useT } from "@/hooks/useT";

export const LocaleToggle = () => {
  const locale = useLocale((s) => s.locale);
  const toggle = useLocale((s) => s.toggle);
  const t = useT();
  return (
    <Button
      variant="outline"
      size="icon"
      onClick={toggle}
      aria-label={t.header.toggleLanguage}
    >
      <span className="font-mono text-xs font-semibold">
        {locale === "en" ? "EN" : "PT"}
      </span>
    </Button>
  );
};
