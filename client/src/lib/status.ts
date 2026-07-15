import type { CallStatus } from "@/types/call";
import type { SessionState } from "@/types/session";

export type StatusTone = "ok" | "neutral" | "warn" | "danger";

const callTones: Record<CallStatus, StatusTone> = {
  connected: "ok",
  ringing: "neutral",
  starting: "neutral",
  reconnecting: "warn",
  ended: "neutral",
};

const sessionTones: Record<SessionState, StatusTone> = {
  open: "ok",
  qr: "neutral",
  connecting: "neutral",
  logged_out: "danger",
};

export const callStatusTone = (status: CallStatus): StatusTone =>
  callTones[status];

export const sessionStateTone = (state: SessionState): StatusTone =>
  sessionTones[state];

export const callStatusPulse = (status: CallStatus): boolean =>
  status === "connected";

export const sessionStatePulse = (state: SessionState): boolean =>
  state === "open";
