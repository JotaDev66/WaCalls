export const waveLevel = (db: number, floor = 0.25): number => {
  const norm = Math.max(0, Math.min(1, (db + 60) / 60));
  return floor + (1 - floor) * norm;
};
