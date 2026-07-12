export const attachMeter = (
  stream: MediaStream,
  onDb: (db: number) => void,
): (() => void) => {
  const ctx = new AudioContext();
  const src = ctx.createMediaStreamSource(stream);
  const analyser = ctx.createAnalyser();
  analyser.fftSize = 1024;
  src.connect(analyser);
  const data = new Float32Array(analyser.fftSize);
  let stopped = false;
  const tick = () => {
    if (stopped) return;
    analyser.getFloatTimeDomainData(data);
    let sum = 0;
    for (let i = 0; i < data.length; i += 1) sum += data[i] * data[i];
    const rms = Math.sqrt(sum / data.length);
    const db = rms > 0 ? Math.max(-60, Math.min(0, 20 * Math.log10(rms))) : -60;
    onDb(db);
    requestAnimationFrame(tick);
  };
  tick();
  return () => {
    stopped = true;
    try {
      src.disconnect();
      analyser.disconnect();
      ctx.close();
    } catch {}
  };
};
