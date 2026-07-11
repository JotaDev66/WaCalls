import { apiPost } from "./api";
import { setupAudioChannel } from "./call/audio-channel";

export type OpenCall = {
  pc: RTCPeerConnection;
  micStream: MediaStream;
  remoteStream: MediaStream | null;
  close: () => void;
};

export const openCall = async (
  sid: string,
  callId: string,
  micDeviceId: string | null,
): Promise<OpenCall> => {
  const localStream = await navigator.mediaDevices.getUserMedia({
    audio: micDeviceId ? { deviceId: { exact: micDeviceId } } : true,
    video: false,
  });

  const pc = new RTCPeerConnection({ iceServers: [] });
  const audio = await setupAudioChannel(pc, localStream);

  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);
  await new Promise<void>((resolve) => {
    if (pc.iceGatheringState === "complete") resolve();
    else
      pc.addEventListener("icegatheringstatechange", () => {
        if (pc.iceGatheringState === "complete") resolve();
      });
  });

  const { sdp_answer } = await apiPost<{ sdp_answer: string }>(
    `/api/sessions/${sid}/calls/${callId}/webrtc`,
    { sdp_offer: pc.localDescription!.sdp },
  );
  await pc.setRemoteDescription({ type: "answer", sdp: sdp_answer });

  return {
    pc,
    micStream: localStream,
    remoteStream: audio.remoteStream,
    close: () => {
      audio.close();
      try {
        localStream.getTracks().forEach((t) => t.stop());
      } catch {}
      try {
        pc.close();
      } catch {}
    },
  };
};
