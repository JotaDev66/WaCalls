export type HistoryRow = {
  callId: string;
  peer: string;
  peerName?: string;
  peerPhotoUrl?: string;
  direction: "inbound" | "outbound";
  startedAt: number;
  endedAt: number | null;
  endReason: string | null;
};

export type HistoryPage = {
  calls: HistoryRow[];
  nextCursor?: string;
};
