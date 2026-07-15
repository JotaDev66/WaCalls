import { apiGet, apiPost } from "@/lib/api";

export type AuthStatus = { authenticated: boolean };

export const fetchAuthStatus = () => apiGet<AuthStatus>("/api/auth/status");

export const login = (username: string, password: string) =>
  apiPost<{ status: string }>("/api/login", { username, password });

export const logout = () => apiPost<unknown>("/api/logout", {});

export const changePassword = (currentPassword: string, newPassword: string) =>
  apiPost<unknown>("/api/auth/password", { currentPassword, newPassword });
