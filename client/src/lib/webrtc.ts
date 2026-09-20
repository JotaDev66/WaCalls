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
import { VIDEO_CHANNEL_LABEL, VIDEO_CTL_KEYFRAME_REQUEST } from "../constants/video";
import { startVideoPipe, videoCallSupported, type VideoPipe } from "./video-pipe";

export type OpenCall = {
  pc: RTCPeerConnection;
  micStream: MediaStream;
  remoteStream: MediaStream | null;
  // Setados quando a chamada tem vídeo (desde o início, ou depois de um
  // upgradeToVideo numa chamada que começou só de áudio).
  localVideoStream: MediaStream | null;
  remoteVideoStream: MediaStream | null;
  // upgradeToVideo abre o data channel "vp8" numa chamada já em curso, via
  // renegociação WebRTC de verdade (a peer connection já existe e o áudio
  // já está fluindo — não dá pra simplesmente recriar o bridge). No-op se a
  // chamada já tem vídeo. Só cuida do transporte; quem chama ainda precisa
  // avisar o backend (POST .../video/start) pra sinalizar o upgrade pro
  // WhatsApp — ver useUpgradeToVideo em hooks/useCallActions.ts.
  upgradeToVideo: (camDeviceId: string | null) => Promise<void>;
  // downgradeFromVideo para a captura da câmera local. Não fecha o canal
  // "vp8" (evitar mais uma renegociação) nem avisa o backend sozinho — ver
  // useDowngradeFromVideo.
  downgradeFromVideo: () => void;
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

  // wireVideoDataChannel abre a câmera e liga um canal "vp8" (já criado, em
  // qualquer estado) ao pipe de vídeo — usado tanto na chamada inicial
  // (canal precisa existir antes do createOffer pra entrar no SDP) quanto
  // num upgrade no meio da chamada (canal novo, criado sob demanda).
  const wireVideoDataChannel = async (
    vdc: RTCDataChannel,
    camDeviceId: string | null,
  ): Promise<VideoPipe> => {
    let sendErrors = 0;
    const pipe = await startVideoPipe({
      camDeviceId,
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
    vdc.onmessage = (e: MessageEvent<ArrayBuffer>) => {
      // Mensagem de controle de 1 byte: pedido de keyframe (PLI/FIR do peer).
      if (e.data.byteLength === 1 && new Uint8Array(e.data)[0] === VIDEO_CTL_KEYFRAME_REQUEST) {
        pipe.requestKeyframe();
        return;
      }
      pipe.pushEncodedFrame(e.data);
    };
    return pipe;
  };

  // O canal "vp8" tem que existir antes do createOffer para entrar no SDP.
  let videoPipe: VideoPipe | null = null;
  let videoDc: RTCDataChannel | null = null;
  if (wantVideo) {
    videoDc = pc.createDataChannel(VIDEO_CHANNEL_LABEL, { ordered: true });
    videoDc.binaryType = "arraybuffer";
    try {
      videoPipe = await wireVideoDataChannel(videoDc, opts.camDeviceId ?? null);
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

  const conn: OpenCall = {
    pc,
    micStream,
    remoteStream: streamDest.stream,
    localVideoStream: videoPipe?.localStream ?? null,
    remoteVideoStream: videoPipe?.remoteStream ?? null,
    upgradeToVideo: async (camDeviceId) => {
      if (videoPipe) return; // já é vídeo
      if (!videoCallSupported()) {
        throw new Error("este browser não suporta vídeo (precisa de WebCodecs/Chrome)");
      }
      const vdc = pc.createDataChannel(VIDEO_CHANNEL_LABEL, { ordered: true });
      vdc.binaryType = "arraybuffer";
      const pipe = await wireVideoDataChannel(vdc, camDeviceId);
      videoDc = vdc;
      videoPipe = pipe;
      conn.localVideoStream = pipe.localStream;
      conn.remoteVideoStream = pipe.remoteStream;

      const upgradeOffer = await pc.createOffer();
      await pc.setLocalDescription(upgradeOffer);
      await new Promise<void>((resolve) => {
        if (pc.iceGatheringState === "complete") resolve();
        else
          pc.addEventListener("icegatheringstatechange", () => {
            if (pc.iceGatheringState === "complete") resolve();
          });
      });
      const { sdp_answer: renegotiatedAnswer } = await apiPost<{ sdp_answer: string }>(
        `/api/sessions/${sid}/calls/${callId}/webrtc/renegotiate`,
        { sdp_offer: pc.localDescription!.sdp },
      );
      await pc.setRemoteDescription({ type: "answer", sdp: renegotiatedAnswer });
    },
    downgradeFromVideo: () => {
      if (!videoPipe) return;
      try {
        videoPipe.close();
      } catch {}
      videoPipe = null;
      conn.localVideoStream = null;
      conn.remoteVideoStream = null;
    },
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
  return conn;
};
