import { useState } from "react";
import { useAuth, clearTokenPrompt } from "@/stores/auth";
import { setToken } from "@/lib/api-token";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useT } from "@/hooks/useT";

export const AuthGate = () => {
  const needsToken = useAuth((s) => s.needsToken);
  const [value, setValue] = useState("");
  const t = useT();
  if (!needsToken) return null;

  const submit = () => {
    const t = value.trim();
    if (!t) return;
    setToken(t);
    clearTokenPrompt();
    window.location.reload();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur">
      <div className="w-full max-w-sm space-y-4 rounded-lg border bg-card p-6 shadow-lg">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">{t.auth.title}</h2>
          <p className="text-sm text-muted-foreground">{t.auth.description}</p>
        </div>
        <Input
          type="password"
          value={value}
          autoFocus
          placeholder={t.auth.tokenPlaceholder}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <Button className="w-full" onClick={submit}>
          {t.auth.submit}
        </Button>
      </div>
    </div>
  );
};
