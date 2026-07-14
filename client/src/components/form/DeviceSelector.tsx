import { Mic, Volume2 } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAudioDevices } from "@/hooks/useAudioDevices";
import { useDevices } from "@/stores/devices";
import type { AudioDevice } from "@/hooks/useAudioDevices";

const DEFAULT_VALUE = "__default__";

const DeviceSelect = ({
  icon,
  devices,
  value,
  defaultLabel,
  onChange,
}: {
  icon: React.ReactNode;
  devices: AudioDevice[];
  value: string | null;
  defaultLabel: string;
  onChange: (id: string) => void;
}) => (
  <div className="inline-flex items-center gap-2">
    {icon}
    <Select
      value={value ?? DEFAULT_VALUE}
      onValueChange={(v) => onChange(v === DEFAULT_VALUE ? "" : v)}
    >
      <SelectTrigger className="w-auto min-w-[160px] max-w-[240px]">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={DEFAULT_VALUE}>{defaultLabel}</SelectItem>
        {devices
          .filter((d) => d.deviceId !== "")
          .map((d) => (
            <SelectItem key={d.deviceId} value={d.deviceId}>
              {d.label}
            </SelectItem>
          ))}
      </SelectContent>
    </Select>
  </div>
);

export const DeviceSelector = () => {
  const { mics, outs } = useAudioDevices();
  const micId = useDevices((s) => s.micId);
  const outId = useDevices((s) => s.outId);
  const setMic = useDevices((s) => s.setMic);
  const setOut = useDevices((s) => s.setOut);

  return (
    <div className="flex flex-wrap items-center gap-3">
      <DeviceSelect
        icon={<Mic className="h-4 w-4 text-muted-foreground" />}
        devices={mics}
        value={micId}
        defaultLabel="Default mic"
        onChange={setMic}
      />
      <DeviceSelect
        icon={<Volume2 className="h-4 w-4 text-muted-foreground" />}
        devices={outs}
        value={outId}
        defaultLabel="Default speaker"
        onChange={setOut}
      />
    </div>
  );
};
