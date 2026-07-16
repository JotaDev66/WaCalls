import { getClientId } from "./client-id";

let onUnauthorized: () => void = () => {};
export const setOnUnauthorized = (fn: () => void): void => {
  onUnauthorized = fn;
};

const baseHeaders = (): HeadersInit => ({
  "X-Client-Id": getClientId(),
  "Content-Type": "application/json",
});

const guard = (status: number): void => {
  if (status === 401) {
    onUnauthorized();
  }
};

export const apiGet = async <T>(path: string): Promise<T> => {
  const r = await fetch(path, {
    headers: baseHeaders(),
    credentials: "same-origin",
  });
  if (!r.ok) {
    guard(r.status);
    throw new Error(`${path} ${r.status}`);
  }
  return r.json() as Promise<T>;
};

export const apiGetBlob = async (path: string): Promise<Blob> => {
  const r = await fetch(path, {
    headers: baseHeaders(),
    credentials: "same-origin",
  });
  if (!r.ok) {
    guard(r.status);
    throw new Error(`${path} ${r.status}`);
  }
  return r.blob();
};

export const apiPost = async <T>(path: string, body: unknown): Promise<T> => {
  const r = await fetch(path, {
    method: "POST",
    headers: baseHeaders(),
    body: JSON.stringify(body),
    credentials: "same-origin",
  });
  if (!r.ok) {
    guard(r.status);
    const text = await r.text().catch(() => "");
    throw new Error(`${path} ${r.status} ${text}`);
  }
  if (r.status === 204) return null as T;
  return r.json() as Promise<T>;
};

export const apiPatch = async (path: string, body: unknown): Promise<void> => {
  const r = await fetch(path, {
    method: "PATCH",
    headers: baseHeaders(),
    body: JSON.stringify(body),
    credentials: "same-origin",
  });
  if (!r.ok) {
    guard(r.status);
    const text = await r.text().catch(() => "");
    throw new Error(`${path} ${r.status} ${text}`);
  }
};

export const apiDelete = async (path: string): Promise<void> => {
  const r = await fetch(path, {
    method: "DELETE",
    headers: baseHeaders(),
    credentials: "same-origin",
  });
  if (!r.ok) {
    guard(r.status);
    throw new Error(`${path} ${r.status}`);
  }
};
