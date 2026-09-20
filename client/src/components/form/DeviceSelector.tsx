import { Mic, Video, Volume2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { useAudioDevices } from "@/hooks/useAudioDevices";
import { useVideoDevices } from "@/hooks/useVideoDevices";
import { useDevices } from "@/stores/devices";

const selectClass = cn(
  "h-9 rounded-md border border-input bg-transparent px-2 text-sm shadow-sm",
  "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
);

export const DeviceSelector = () => {
  const { mics, outs } = useAudioDevices();
  const { cams } = useVideoDevices();
  const micId = useDevices((s) => s.micId);
  const outId = useDevices((s) => s.outId);
  const camId = useDevices((s) => s.camId);
  const setMic = useDevices((s) => s.setMic);
  const setOut = useDevices((s) => s.setOut);
  const setCam = useDevices((s) => s.setCam);

  return (
    <div className="flex flex-wrap items-center gap-3">
      <div className="inline-flex items-center gap-2">
        <Mic className="h-4 w-4 text-muted-foreground" />
        <select value={micId ?? ""} onChange={(e) => setMic(e.target.value)} className={selectClass}>
          <option value="">Default mic</option>
          {mics.map((d) => (
            <option key={d.deviceId} value={d.deviceId}>
              {d.label}
            </option>
          ))}
        </select>
      </div>
      <div className="inline-flex items-center gap-2">
        <Volume2 className="h-4 w-4 text-muted-foreground" />
        <select value={outId ?? ""} onChange={(e) => setOut(e.target.value)} className={selectClass}>
          <option value="">Default speaker</option>
          {outs.map((d) => (
            <option key={d.deviceId} value={d.deviceId}>
              {d.label}
            </option>
          ))}
        </select>
      </div>
      {cams.length > 0 && (
        <div className="inline-flex items-center gap-2">
          <Video className="h-4 w-4 text-muted-foreground" />
          <select value={camId ?? ""} onChange={(e) => setCam(e.target.value)} className={selectClass}>
            <option value="">Default camera</option>
            {cams.map((d) => (
              <option key={d.deviceId} value={d.deviceId}>
                {d.label}
              </option>
            ))}
          </select>
        </div>
      )}
    </div>
  );
};
