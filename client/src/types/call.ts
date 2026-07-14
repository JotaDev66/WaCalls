export type CallStatus =
  "starting" | "ringing" | "connected" | "reconnecting" | "ended";

export type CallSummary = {
  sessionId: string;
  callId: string;
  owner: string | null;
  direction: "outbound" | "inbound";
  peer: string;
  startedAt: number;
  status: CallStatus;
};

export type IncomingPayload = {
  sessionId: string;
  callId: string;
  peer: string;
  offeredAt: number;
};

export type QualitySample = {
  rttMs: number;
  jitterMs: number;
  lossFraction: number;
  hasRtt: boolean;
};
