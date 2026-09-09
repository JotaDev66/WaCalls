import { apiPost } from "./api";
import { float32ToInt16LE, int16LEToFloat32 } from "./pcm";
import {
  CAPTURE_PROCESSOR_NAME,
  CAPTURE_WORKLET_URL,
  PCM_CHANNEL_LABEL,
  PLAYBACK_PROCESSOR_NAME,
  PLAYBACK_WORKLET_URL,
  SAMPLE_RATE,
} from "../constants/audio";
import { VIDEO_CHANNEL_LABEL } from "../constants/video";
import { startVideoPipe, videoCallSupported, type VideoPipe } from "./video-pipe";

export type OpenCall = {
  pc: RTCPeerConnection;
  micStream: MediaStream;
  remoteStream: MediaStream | null;
  // Set only on a video call: the local camera and the decoded peer video.
  localVideoStream: MediaStream | null;
  remoteVideoStream: MediaStream | null;
  close: () => void;
};

export type OpenCallOpts = {
  video?: boolean;
  camDeviceId?: string | null;
};

export const openCall = async (
  sid: string,
  callId: string,
  micDeviceId: string | null,
  opts: OpenCallOpts = {},
): Promise<OpenCall> => {
  const wantVideo = !!opts.video;
  if (wantVideo && !videoCallSupported()) {
    throw new Error("This browser can't do video calls (needs WebCodecs / Chrome).");
  }

  const micStream = await navigator.mediaDevices.getUserMedia({
    audio: micDeviceId ? { deviceId: { exact: micDeviceId } } : true,
  });

  const pc = new RTCPeerConnection({ iceServers: [] });

  const dc = pc.createDataChannel(PCM_CHANNEL_LABEL, { ordered: true });
  dc.binaryType = "arraybuffer";

  // O canal "vp8" tem que existir antes do createOffer para entrar no SDP.
  let videoPipe: VideoPipe | null = null;
  let videoDc: RTCDataChannel | null = null;
  if (wantVideo) {
    videoDc = pc.createDataChannel(VIDEO_CHANNEL_LABEL, { ordered: true });
    videoDc.binaryType = "arraybuffer";
    const vdc = videoDc;
    let sendErrors = 0;
    try {
      videoPipe = await startVideoPipe({
        camDeviceId: opts.camDeviceId ?? null,
        onEncoded: (msg) => {
          if (vdc.readyState !== "open") return; // quadros antes do canal abrir são descartados
          try {
            vdc.send(msg);
          } catch (e) {
            sendErrors += 1;
            if (sendErrors <= 3) console.error("[video] falha no vdc.send", e);
          }
        },
      });
      const pipe = videoPipe;
      videoDc.onmessage = (e: MessageEvent<ArrayBuffer>) => pipe.pushEncodedFrame(e.data);
    } catch (err) {
      micStream.getTracks().forEach((t) => t.stop());
      pc.close();
      throw err instanceof Error ? err : new Error("câmera indisponível");
    }
  }

  const ctx = new AudioContext({ sampleRate: SAMPLE_RATE });
  await ctx.audioWorklet.addModule(CAPTURE_WORKLET_URL);
  await ctx.audioWorklet.addModule(PLAYBACK_WORKLET_URL);
  await ctx.resume();

  const micSource = ctx.createMediaStreamSource(micStream);
  const captureNode = new AudioWorkletNode(ctx, CAPTURE_PROCESSOR_NAME);
  captureNode.port.onmessage = (e: MessageEvent<Float32Array>) => {
    if (dc.readyState === "open") dc.send(float32ToInt16LE(e.data));
  };
  micSource.connect(captureNode);
  captureNode.connect(ctx.destination);

  const playbackNode = new AudioWorkletNode(ctx, PLAYBACK_PROCESSOR_NAME);
  const streamDest = ctx.createMediaStreamDestination();
  playbackNode.connect(streamDest);
  dc.onmessage = (e: MessageEvent<ArrayBuffer>) => {
    playbackNode.port.postMessage(int16LEToFloat32(e.data));
  };

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
    micStream,
    remoteStream: streamDest.stream,
    localVideoStream: videoPipe?.localStream ?? null,
    remoteVideoStream: videoPipe?.remoteStream ?? null,
    close: () => {
      try {
        videoPipe?.close();
      } catch {}
      try {
        micStream.getTracks().forEach((t) => t.stop());
      } catch {}
      try {
        ctx.close();
      } catch {}
      try {
        pc.close();
      } catch {}
    },
  };
};
