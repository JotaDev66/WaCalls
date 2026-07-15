import { useState } from "react";
import { login } from "@/services/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/hooks/useT";

export const LoginForm = () => {
  const t = useT();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username.trim() || !password) return;
    setBusy(true);
    setError(false);
    try {
      await login(username, password);
      window.location.reload();
    } catch {
      setError(true);
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur">
      <form
        onSubmit={submit}
        className="w-full max-w-sm space-y-4 rounded-lg border bg-card p-6 shadow-lg"
      >
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">{t.login.title}</h2>
          <p className="text-sm text-muted-foreground">{t.login.description}</p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="login-user">{t.login.user}</Label>
          <Input
            id="login-user"
            value={username}
            autoFocus
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="login-pass">{t.login.password}</Label>
          <Input
            id="login-pass"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        {error && <p className="text-sm text-destructive">{t.login.error}</p>}
        <Button className="w-full" type="submit" disabled={busy}>
          {busy ? t.login.submitting : t.login.submit}
        </Button>
      </form>
    </div>
  );
};
