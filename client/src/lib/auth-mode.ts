export type AuthStatus = {
  mode: "open" | "token" | "login";
  authenticated: boolean;
};

export function gateFor(s: AuthStatus): "none" | "login" | "token" {
  if (s.mode === "open" || s.authenticated) return "none";
  return s.mode === "login" ? "login" : "token";
}
