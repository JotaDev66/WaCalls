import type { CallStatus } from "@/types/call";

export const formatCallDuration = (
  startedAt: number,
  status: CallStatus,
): string => {
  if (status !== "connected") return status;
  const s = Math.floor((Date.now() - startedAt) / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
};
