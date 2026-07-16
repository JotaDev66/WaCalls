export type CallStatus =
  "starting" | "ringing" | "connected" | "reconnecting" | "ended";

export type CallSummary = {
  sessionId: string;
  callId: string;
  owner: string | null;
  direction: "outbound" | "inbound";
  peer: string;
  peerName?: string;
  peerPhotoUrl?: string;
  startedAt: number;
  status: CallStatus;
};

export type IncomingPayload = {
  sessionId: string;
  callId: string;
  peer: string;
  peerName?: string;
  peerPhotoUrl?: string;
  offeredAt: number;
};

export type QualitySample = {
  rttMs: number;
  jitterMs: number;
  lossFraction: number;
  hasRtt: boolean;
};

export type SetupMark = {
  mark: string;
  elapsedMs: number;
};
