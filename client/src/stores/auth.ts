import { create } from "zustand";
import type { AuthStatus } from "@/services/auth";

type State = { status: AuthStatus | null };

export const useAuth = create<State>(() => ({ status: null }));

export const setAuthStatus = (status: AuthStatus): void =>
  useAuth.setState({ status });
