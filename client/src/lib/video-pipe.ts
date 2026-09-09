import {
  VIDEO_BITRATE,
  VIDEO_FRAMERATE,
  VIDEO_H264_CODEC,
  VIDEO_HEIGHT,
  VIDEO_KEYFRAME_INTERVAL_MS,
  VIDEO_MAX_EDGE,
  VIDEO_WIDTH,
} from "@/constants/video";
import { decodeVideoFrame, encodeVideoFrame } from "./video-frame";

// videoCallSupported diz se o browser tem as peças do WebCodecs que o pipe de
// vídeo precisa. Só navegadores Chromium se qualificam hoje, o que combina com o
// resto do app (já depende de HTMLMediaElement.setSinkId).
export const videoCallSupported = (): boolean =>
  typeof VideoEncoder !== "undefined" &&
  typeof VideoDecoder !== "undefined" &&
  typeof VideoFrame !== "undefined" &&
  typeof EncodedVideoChunk !== "undefined" &&
  typeof HTMLVideoElement !== "undefined";

export type VideoPipe = {
  // localStream é a captura crua da câmera, para o <video> de preview local.
  localStream: MediaStream;
  // remoteStream carrega o vídeo do peer já decodificado, para o <video> remoto.
  remoteStream: MediaStream;
  // pushEncodedFrame entrega uma mensagem do data channel "vp8" ao decoder.
  pushEncodedFrame: (msg: ArrayBuffer) => void;
  close: () => void;
};

type StartOpts = {
  camDeviceId: string | null;
  // onEncoded recebe cada quadro codificado já serializado para o data channel.
  onEncoded: (msg: ArrayBuffer) => void;
};

const even = (n: number) => Math.max(2, Math.floor(n / 2) * 2);

// startVideoPipe abre a câmera, sobe um encoder H264 alimentado por um timer e um
// decoder H264 que desenha num canvas fora do DOM exposto como remoteStream.
// Lança se o browser não tem WebCodecs ou a câmera está indisponível; o chamador
// deve cair para áudio-só.
export const startVideoPipe = async ({ camDeviceId, onEncoded }: StartOpts): Promise<VideoPipe> => {
  if (!videoCallSupported()) throw new Error("este browser não tem suporte a vídeo por WebCodecs");

  const localStream = await navigator.mediaDevices.getUserMedia({
    video: {
      deviceId: camDeviceId ? { exact: camDeviceId } : undefined,
      width: { ideal: VIDEO_WIDTH },
      height: { ideal: VIDEO_HEIGHT },
      frameRate: { ideal: VIDEO_FRAMERATE },
    },
  });

  // Todo quadro da câmera é redesenhado num canvas fixo (proporção mantida,
  // aresta maior <= VIDEO_MAX_EDGE) e o encoder é configurado nesse tamanho.
  // Dois motivos: o H264 do WebCodecs não escala (um quadro de tamanho diferente
  // do configurado lança erro em todo encode()), e um keyframe em resolução
  // cheia estouraria o maxMessageSize do SCTP do data channel.
  const track = localStream.getVideoTracks()[0];
  const st = track?.getSettings() ?? {};
  const srcW = st.width ?? VIDEO_WIDTH;
  const srcH = st.height ?? VIDEO_HEIGHT;
  const scale = Math.min(1, VIDEO_MAX_EDGE / Math.max(srcW, srcH));
  const encW = even(srcW * scale);
  const encH = even(srcH * scale);
  const encFps = Math.round(st.frameRate ?? VIDEO_FRAMERATE);

  const scaleCanvas = document.createElement("canvas");
  scaleCanvas.width = encW;
  scaleCanvas.height = encH;
  const scaleCtx = scaleCanvas.getContext("2d");

  let closed = false;

  // --- codificação: câmera -> VideoEncoder -> data channel ---------------
  const captureEl = document.createElement("video");
  captureEl.muted = true;
  captureEl.playsInline = true;
  captureEl.srcObject = localStream;

  let encodeErrors = 0;
  const encoder = new VideoEncoder({
    output: (chunk) => {
      const buf = new ArrayBuffer(chunk.byteLength);
      chunk.copyTo(buf);
      onEncoded(
        encodeVideoFrame({
          keyframe: chunk.type === "key",
          // a câmera é sempre capturada na vertical, então sem rotação na saída.
          rotationDeg: 0,
          // chunk.timestamp vem em microssegundos; o formato de fio é em ms.
          timestampMs: Math.round(chunk.timestamp / 1000) >>> 0,
          data: new Uint8Array(buf),
        }),
      );
    },
    error: (e) => {
      encodeErrors += 1;
      if (encodeErrors <= 3) console.error("[video] erro no encoder", e);
    },
  });
  encoder.configure({
    codec: VIDEO_H264_CODEC,
    avc: { format: "annexb" },
    latencyMode: "realtime",
    width: encW,
    height: encH,
    bitrate: VIDEO_BITRATE,
    framerate: encFps,
  });

  let lastKeyframeAt = 0;
  // Loop de captura por setInterval em vez de requestVideoFrameCallback: o rvfc
  // num <video> fora do DOM às vezes trava no Chrome e a câmera nunca é
  // codificada. O interval dispara sempre.
  const onCaptureFrame = (): void => {
    if (closed || encoder.state !== "configured" || !scaleCtx) return;
    if (captureEl.readyState < 2) return; // ainda sem quadro disponível
    if (encoder.encodeQueueSize >= 2) return; // encoder atrasado: descarta
    let frame: VideoFrame | null = null;
    try {
      scaleCtx.drawImage(captureEl, 0, 0, encW, encH);
      frame = new VideoFrame(scaleCanvas, { timestamp: Math.round(performance.now() * 1000) });
      const now = performance.now();
      const keyFrame = lastKeyframeAt === 0 || now - lastKeyframeAt >= VIDEO_KEYFRAME_INTERVAL_MS;
      if (keyFrame) lastKeyframeAt = now;
      encoder.encode(frame, { keyFrame });
    } catch (e) {
      console.error("[video] falha ao codificar", e);
    } finally {
      frame?.close();
    }
  };
  await captureEl.play().catch((e) => console.error("[video] captureEl.play falhou", e));
  const captureTimer = window.setInterval(onCaptureFrame, Math.max(20, Math.round(1000 / encFps)));

  // --- decodificação: data channel -> VideoDecoder -> canvas -> remoteStream
  const canvas = document.createElement("canvas");
  canvas.width = encW;
  canvas.height = encH;
  const ctx = canvas.getContext("2d");

  // rotação (graus, horário) do último quadro recebido, vinda da extensão CVO.
  let peerRotation: 0 | 90 | 180 | 270 = 0;
  const buildDecoder = (): VideoDecoder => {
    const d = new VideoDecoder({
      output: (frame) => {
        try {
          if (ctx) {
            const fw = frame.displayWidth || canvas.width;
            const fh = frame.displayHeight || canvas.height;
            const swap = peerRotation === 90 || peerRotation === 270;
            const cw = swap ? fh : fw;
            const ch = swap ? fw : fh;
            if (canvas.width !== cw) canvas.width = cw;
            if (canvas.height !== ch) canvas.height = ch;
            ctx.save();
            ctx.translate(canvas.width / 2, canvas.height / 2);
            // CVO do WhatsApp: girar no sentido anti-horário para corrigir a orientação.
            ctx.rotate((-peerRotation * Math.PI) / 180);
            ctx.drawImage(frame, -fw / 2, -fh / 2, fw, fh);
            ctx.restore();
          }
        } finally {
          frame.close();
        }
      },
      error: (e) => console.error("[video] erro no decoder", e),
    });
    // Stream Annex-B com SPS/PPS embutido em todo keyframe → sem `description`.
    d.configure({ codec: VIDEO_H264_CODEC, optimizeForLatency: true });
    return d;
  };

  let decoder = buildDecoder();
  let sawKeyframe = false;

  const pushEncodedFrame = (msg: ArrayBuffer): void => {
    if (closed) return;
    const f = decodeVideoFrame(msg);
    if (!f) return;
    peerRotation = f.rotationDeg;
    if (!sawKeyframe && !f.keyframe) return; // um decoder só arranca num keyframe
    sawKeyframe = true;
    try {
      if (decoder.state !== "configured") {
        decoder = buildDecoder();
        if (!f.keyframe) {
          sawKeyframe = false;
          return;
        }
      }
      decoder.decode(
        new EncodedVideoChunk({
          type: f.keyframe ? "key" : "delta",
          timestamp: f.timestampMs * 1000,
          data: f.data,
        }),
      );
    } catch (e) {
      console.error("[video] falha ao decodificar, resetando", e);
      try {
        decoder.close();
      } catch {}
      decoder = buildDecoder();
      sawKeyframe = false;
    }
  };

  const remoteStream = canvas.captureStream(encFps);

  const close = (): void => {
    if (closed) return;
    closed = true;
    clearInterval(captureTimer);
    captureEl.srcObject = null;
    for (const t of localStream.getTracks()) t.stop();
    for (const t of remoteStream.getTracks()) t.stop();
    try {
      if (encoder.state !== "closed") encoder.close();
    } catch {}
    try {
      if (decoder.state !== "closed") decoder.close();
    } catch {}
  };

  return { localStream, remoteStream, pushEncodedFrame, close };
};
