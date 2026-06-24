import type { CallSummary } from "@/types/call";

export const formatCallDuration = (call: CallSummary): string => {
  if (call.status !== "connected") return call.status;
  // Count from when the call was answered (connectedAt), not the dial time.
  const since = call.connectedAt ?? call.startedAt;
  const s = Math.max(0, Math.floor((Date.now() - since) / 1000));
  return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
};
