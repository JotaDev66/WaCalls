export type CallStatus = "starting" | "ringing" | "connected" | "ended";

export type CallSummary = {
  sessionId: string;
  callId: string;
  owner: string | null;
  direction: "outbound" | "inbound";
  peer: string;
  startedAt: number;
  connectedAt?: number | null;
  status: CallStatus;
};

export type IncomingPayload = { sessionId: string; callId: string; peer: string; offeredAt: number };
