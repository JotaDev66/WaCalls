import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { openCall } from "@/lib/webrtc";
import { acceptCall, endCall } from "@/services/calls";
import { useDevices } from "@/stores/devices";
import { registerOwnConnection, clearIncoming } from "@/stores/calls";

export const useAcceptCall = (micId: string | null) => {
  const camId = useDevices((s) => s.camId);
  return useMutation({
    mutationFn: async (vars: { sid: string; callId: string; video?: boolean }) => {
      const res = await acceptCall(vars.sid, vars.callId);
      try {
        const conn = await openCall(vars.sid, res.call.callId, micId, {
          video: vars.video,
          camDeviceId: camId,
        });
        registerOwnConnection(res.call.callId, conn);
      } catch (wrtcErr) {
        try {
          await endCall(vars.sid, res.call.callId);
        } catch {}
        throw wrtcErr;
      }
      clearIncoming();
      return res.call.callId;
    },
    onError: (e: Error) => {
      if (e.message.includes("409")) {
        clearIncoming();
        return;
      }
      toast.error(e.message);
    },
  });
};
