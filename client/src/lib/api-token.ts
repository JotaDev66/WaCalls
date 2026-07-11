const KEY = "wacalls.apiToken";

export const getToken = (): string => localStorage.getItem(KEY) ?? "";

export const setToken = (t: string): void => localStorage.setItem(KEY, t);

export const clearToken = (): void => localStorage.removeItem(KEY);
