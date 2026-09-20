// Formato de fio do data channel "vp8", espelho byte a byte de
// media.EncodeVideoFrame / media.DecodeVideoFrame em
// internal/voip/media/videoframe.go: 1 byte de flags + 4 bytes de timestamp em
// ms (big-endian) + o bitstream H264 (Annex-B) opaco.
//
// Flags: bit 0 = keyframe; bits 1-2 = rotação (0=0°, 1=90°, 2=180°, 3=270°,
// sentido horário).

export type WireVideoFrame = {
  keyframe: boolean;
  rotationDeg: 0 | 90 | 180 | 270;
  timestampMs: number;
  data: Uint8Array;
};

const HEADER_LEN = 5;
const ROT_BY_CODE: Record<number, 0 | 90 | 180 | 270> = { 0: 0, 1: 90, 2: 180, 3: 270 };
const CODE_BY_ROT: Record<number, number> = { 0: 0, 90: 1, 180: 2, 270: 3 };

export const encodeVideoFrame = (f: WireVideoFrame): ArrayBuffer => {
  const out = new Uint8Array(HEADER_LEN + f.data.byteLength);
  const view = new DataView(out.buffer);
  out[0] = (f.keyframe ? 1 : 0) | ((CODE_BY_ROT[f.rotationDeg] ?? 0) << 1);
  view.setUint32(1, f.timestampMs >>> 0, false);
  out.set(f.data, HEADER_LEN);
  return out.buffer;
};

// decodeVideoFrame faz o parse de uma mensagem do data channel. Retorna null
// quando a mensagem é curta demais para o cabeçalho (equivale a ok=false no Go).
export const decodeVideoFrame = (buf: ArrayBuffer): WireVideoFrame | null => {
  if (buf.byteLength < HEADER_LEN) return null;
  const view = new DataView(buf);
  const flags = view.getUint8(0);
  return {
    keyframe: (flags & 1) !== 0,
    rotationDeg: ROT_BY_CODE[(flags >> 1) & 3] ?? 0,
    timestampMs: view.getUint32(1, false),
    data: new Uint8Array(buf, HEADER_LEN),
  };
};
