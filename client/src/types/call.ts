export type CallStatus = "starting" | "ringing" | "connected" | "ended";

export type CallMedia = "audio" | "video";

export type CallSummary = {
  sessionId: string;
  callId: string;
  owner: string | null;
  direction: "outbound" | "inbound";
  peer: string;
  media: CallMedia;
  startedAt: number;
  status: CallStatus;
};

export type IncomingPayload = {
  sessionId: string;
  callId: string;
  peer: string;
  media: CallMedia;
  offeredAt: number;
};
