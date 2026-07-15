import { create } from "zustand";
import type { AuthStatus } from "@/lib/auth-mode";

type State = { needsToken: boolean; status: AuthStatus | null };

export const useAuth = create<State>(() => ({
  needsToken: false,
  status: null,
}));

export const promptForToken = (): void =>
  useAuth.setState({ needsToken: true });

export const clearTokenPrompt = (): void =>
  useAuth.setState({ needsToken: false });

export const setAuthStatus = (status: AuthStatus): void =>
  useAuth.setState({ status });
