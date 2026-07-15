import { useEffect, useState } from "react";
import { useAuth, clearTokenPrompt, setAuthStatus } from "@/stores/auth";
import { setToken } from "@/lib/api-token";
import { fetchAuthStatus } from "@/services/auth";
import { gateFor } from "@/lib/auth-mode";
import { LoginForm } from "@/components/domain/auth/LoginForm";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useT } from "@/hooks/useT";

export const AuthGate = () => {
  const t = useT();
  const needsToken = useAuth((s) => s.needsToken);
  const status = useAuth((s) => s.status);
  const [value, setValue] = useState("");

  useEffect(() => {
    let alive = true;
    fetchAuthStatus()
      .then((s) => alive && setAuthStatus(s))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  const gate = status ? gateFor(status) : "none";

  if (gate === "login") return <LoginForm />;

  if (gate === "token" || needsToken) {
    const submit = () => {
      const tok = value.trim();
      if (!tok) return;
      setToken(tok);
      clearTokenPrompt();
      window.location.reload();
    };
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur">
        <div className="w-full max-w-sm space-y-4 rounded-lg border bg-card p-6 shadow-lg">
          <div className="space-y-1">
            <h2 className="text-lg font-semibold">{t.auth.title}</h2>
            <p className="text-sm text-muted-foreground">
              {t.auth.description}
            </p>
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
  }

  return null;
};
