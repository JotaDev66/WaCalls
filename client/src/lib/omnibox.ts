export type OmniItem =
  | { kind: "dial"; phone: string }
  | { kind: "session"; id: string; name: string }
  | { kind: "new-session" };

export const looksLikePhone = (query: string): boolean => {
  const q = query.trim();
  if (!q || !/^[\d\s()+-]+$/.test(q)) return false;
  return q.replace(/[\s()+-]/g, "").length >= 4;
};

type SessionLite = { id: string; name: string };

export const omniboxItems = (
  query: string,
  sessions: SessionLite[],
  canDial: boolean,
): OmniItem[] => {
  const q = query.trim();
  if (canDial && looksLikePhone(q)) {
    return [{ kind: "dial", phone: q }];
  }
  const lower = q.toLowerCase();
  const matched: OmniItem[] = sessions
    .filter((s) => !lower || s.name.toLowerCase().includes(lower))
    .map((s) => ({ kind: "session", id: s.id, name: s.name }));
  return [...matched, { kind: "new-session" }];
};
