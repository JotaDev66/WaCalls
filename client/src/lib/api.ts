import { getClientId } from "./client-id";
import { getToken, clearToken } from "./api-token";

let onUnauthorized: () => void = () => {};
export const setOnUnauthorized = (fn: () => void): void => {
  onUnauthorized = fn;
};

const baseHeaders = (): HeadersInit => {
  const h: Record<string, string> = {
    "X-Client-Id": getClientId(),
    "Content-Type": "application/json",
  };
  const token = getToken();
  if (token) h["Authorization"] = `Bearer ${token}`;
  return h;
};

const guard = (status: number): void => {
  if (status === 401) {
    clearToken();
    onUnauthorized();
  }
};

export const apiGet = async <T>(path: string): Promise<T> => {
  const r = await fetch(path, { headers: baseHeaders() });
  if (!r.ok) {
    guard(r.status);
    throw new Error(`${path} ${r.status}`);
  }
  return r.json() as Promise<T>;
};

export const apiPost = async <T>(path: string, body: unknown): Promise<T> => {
  const r = await fetch(path, {
    method: "POST",
    headers: baseHeaders(),
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    guard(r.status);
    const text = await r.text().catch(() => "");
    throw new Error(`${path} ${r.status} ${text}`);
  }
  return r.json() as Promise<T>;
};

export const apiDelete = async (path: string): Promise<void> => {
  const r = await fetch(path, { method: "DELETE", headers: baseHeaders() });
  if (!r.ok) {
    guard(r.status);
    throw new Error(`${path} ${r.status}`);
  }
};
