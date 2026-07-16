import { Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useTheme } from "@/stores/theme";
import { useT } from "@/hooks/useT";

export const ThemeToggle = () => {
  const { theme, toggle } = useTheme();
  const t = useT();
  return (
    <Button
      variant="outline"
      size="icon"
      onClick={toggle}
      aria-label={t.header.toggleTheme}
    >
      {theme === "dark" ? (
        <Sun className="h-4 w-4" />
      ) : (
        <Moon className="h-4 w-4" />
      )}
    </Button>
  );
};
