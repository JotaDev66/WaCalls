import { apiPost, apiDelete } from "@/lib/api";
import { getClientId } from "@/lib/client-id";

export const startCall = (sid: string, phone: string, record: boolean, video = false) =>
  apiPost<{ call: { callId: string } }>(`/api/sessions/${sid}/calls`, {
    phone,
    duration_ms: 300_000,
    record,
    video,
  });

export const startGroupCall = (sid: string, groupJID: string, video = false) =>
  apiPost<{ call: { callId: string } }>(`/api/sessions/${sid}/calls`, {
    group: groupJID,
    video,
  });

export const sendReaction = (sid: string, callId: string, text: string) =>
  apiPost<void>(`/api/sessions/${sid}/calls/${callId}/reaction`, { text });

export const setHandRaised = (sid: string, callId: string, raised: boolean) =>
  apiPost<void>(`/api/sessions/${sid}/calls/${callId}/hand`, { raised });

export const addParticipant = (sid: string, callId: string, target: string) =>
  apiPost<void>(`/api/sessions/${sid}/calls/${callId}/participants`, { target });

export const startVideo = (sid: string, callId: string) =>
  apiPost<void>(`/api/sessions/${sid}/calls/${callId}/video/start`, {});

export const stopVideo = (sid: string, callId: string) =>
  apiPost<void>(`/api/sessions/${sid}/calls/${callId}/video/stop`, {});

export const acceptVideo = (sid: string, callId: string) =>
  apiPost<void>(`/api/sessions/${sid}/calls/${callId}/video/accept`, {});

export const acceptCall = (sid: string, callId: string) =>
  apiPost<{ call: { callId: string } }>(`/api/sessions/${sid}/calls/${callId}/accept`, {});

export const rejectCall = async (sid: string, callId: string): Promise<void> => {
  const r = await fetch(`/api/sessions/${sid}/calls/${callId}/reject`, {
    method: "POST",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    body: "{}",
  });
  if (!r.ok) throw new Error(`reject ${r.status}`);
};

export const endCall = (sid: string, callId: string) =>
  apiDelete(`/api/sessions/${sid}/calls/${callId}`);
