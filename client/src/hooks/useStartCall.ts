import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { openCall } from "@/lib/webrtc";
import { startCall, endCall } from "@/services/calls";
import { useDevices } from "@/stores/devices";
import { registerOwnConnection } from "@/stores/calls";

export const useStartCall = (sid: string, micId: string | null) => {
  const camId = useDevices((s) => s.camId);
  return useMutation({
    mutationFn: async (vars: { phone: string; record: boolean; video?: boolean }) => {
      const { call } = await startCall(sid, vars.phone, vars.record, vars.video);
      try {
        const conn = await openCall(sid, call.callId, micId, {
          video: vars.video,
          camDeviceId: camId,
        });
        registerOwnConnection(call.callId, conn);
      } catch (wrtcErr) {
        try {
          await endCall(sid, call.callId);
        } catch {}
        throw wrtcErr;
      }
      return call.callId;
    },
    onError: (e: Error) => {
      const m = e.message;
      if (m.includes("429")) toast.error("Limit reached: max concurrent calls.");
      else if (m.includes("503")) toast.error("WhatsApp not paired.");
      else toast.error(m);
    },
  });
};
