import { useEffect } from "react";
import { useAuth, setAuthStatus } from "@/stores/auth";
import { fetchAuthStatus } from "@/services/auth";
import { LoginForm } from "@/components/domain/auth/LoginForm";

export const AuthGate = () => {
  const status = useAuth((s) => s.status);

  useEffect(() => {
    let alive = true;
    fetchAuthStatus()
      .then((s) => alive && setAuthStatus(s))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  if (status && !status.authenticated) return <LoginForm />;
  return null;
};
