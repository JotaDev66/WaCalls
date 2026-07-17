import type { CallStatus } from "@/types/call";

// needsAudioResume reports whether an owned call is live on the server but has no
// local audio leg here, which happens after a page refresh drops the WebRTC bridge.
// It only triggers once the call is established (connected/reconnecting); a call
// still in setup (ringing/starting) is being attached by the start/accept flow and
// must not be re-attached underneath it.
export const needsAudioResume = (
  status: CallStatus,
  hasConnection: boolean,
): boolean =>
  !hasConnection && (status === "connected" || status === "reconnecting");
