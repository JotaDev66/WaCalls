import { useState } from "react";
import { useAuth, clearTokenPrompt } from "@/stores/auth";
import { setToken } from "@/lib/api-token";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export const AuthGate = () => {
  const needsToken = useAuth((s) => s.needsToken);
  const [value, setValue] = useState("");
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
          <h2 className="text-lg font-semibold">Authentication required</h2>
          <p className="text-sm text-muted-foreground">Enter the API token to continue.</p>
        </div>
        <Input
          type="password"
          value={value}
          autoFocus
          placeholder="API token"
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <Button className="w-full" onClick={submit}>
          Save and reload
        </Button>
      </div>
    </div>
  );
};
