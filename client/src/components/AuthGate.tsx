import React, { useEffect, useState } from "react";

type Props = { children: React.ReactNode };

const STORAGE_KEY = "wa_calls_auth_v1";

export const AuthGate: React.FC<Props> = ({ children }) => {
  // Vite env vars are available under import.meta.env
  const env = (import.meta as any).env ?? {};
  const envUser = env.VITE_DASH_USER;
  const envPass = env.VITE_DASH_PASS;

  // If user/pass aren't set, do not require authentication (developer convenience)
  const protect = !!envUser && !!envPass;

  const [authenticated, setAuthenticated] = useState(() => {
    if (!protect) return true;
    try {
      return localStorage.getItem(STORAGE_KEY) === "true";
    } catch {
      return false;
    }
  });

  const [user, setUser] = useState("");
  const [pass, setPass] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!protect) setAuthenticated(true);
  }, [protect]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (user === envUser && pass === envPass) {
      try {
        localStorage.setItem(STORAGE_KEY, "true");
      } catch {
        // ignore
      }
      setAuthenticated(true);
      setError(null);
    } else {
      setError("Usuário ou senha inválidos");
    }
  };

  const logout = () => {
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      // ignore
    }
    setAuthenticated(false);
    window.location.reload();
  };

  if (!protect) return <>{children}</>;

  if (authenticated) {
    return (
      <>
        <div className="fixed top-4 right-4 z-50">
          <button
            onClick={logout}
            className="inline-flex items-center rounded-md bg-red-600 px-3 py-1 text-sm font-medium text-white hover:bg-red-700"
          >
            Logout
          </button>
        </div>
        {children}
      </>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      <div className="w-full max-w-md p-6 rounded-md border bg-panel">
        <h2 className="text-lg font-semibold mb-4">Login</h2>
        <form onSubmit={submit} className="space-y-4">
          <div>
            <label className="block text-sm mb-1">Usuário</label>
            <input
              className="w-full rounded-md border px-3 py-2 bg-transparent"
              value={user}
              onChange={(e) => setUser(e.target.value)}
              autoComplete="username"
            />
          </div>
          <div>
            <label className="block text-sm mb-1">Senha</label>
            <input
              type="password"
              className="w-full rounded-md border px-3 py-2 bg-transparent"
              value={pass}
              onChange={(e) => setPass(e.target.value)}
              autoComplete="current-password"
            />
          </div>
          {error && <div className="text-sm text-destructive">{error}</div>}
          <div className="flex justify-end">
            <button
              type="submit"
              className="inline-flex items-center rounded-md bg-primary px-3 py-1 text-sm font-medium text-white hover:opacity-95"
            >
              Entrar
            </button>
          </div>
        </form>
        <p className="text-xs text-muted mt-4">Protegido por credenciais definidas em VITE_DASH_USER / VITE_DASH_PASS.</p>
      </div>
    </div>
  );
};

export default AuthGate;
