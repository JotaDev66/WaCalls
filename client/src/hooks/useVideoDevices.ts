import { useEffect, useState } from "react";

export type VideoDevice = { deviceId: string; label: string };

// useVideoDevices lists the cameras without forcing a permission prompt on load
// (unlike useAudioDevices) — the camera light turning on unprompted is jarring.
// Labels stay generic until the user grants camera access via a video call, then
// fill in on the next mount.
export const useVideoDevices = () => {
  const [cams, setCams] = useState<VideoDevice[]>([]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      const list = await navigator.mediaDevices.enumerateDevices();
      if (cancelled) return;
      setCams(
        list
          .filter((d) => d.kind === "videoinput")
          .map((d, i) => ({ deviceId: d.deviceId, label: d.label || `Camera ${i + 1}` })),
      );
    };
    void load();
    navigator.mediaDevices.addEventListener("devicechange", load);
    return () => {
      cancelled = true;
      navigator.mediaDevices.removeEventListener("devicechange", load);
    };
  }, []);

  return { cams };
};
