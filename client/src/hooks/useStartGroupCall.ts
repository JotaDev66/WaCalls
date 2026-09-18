import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { openCall } from "@/lib/webrtc";
import { startGroupCall, endCall } from "@/services/calls";
import { useDevices } from "@/stores/devices";
import { registerOwnConnection } from "@/stores/calls";

export const useStartGroupCall = (sid: string, micId: string | null) => {
  const camId = useDevices((s) => s.camId);
  return useMutation({
    mutationFn: async (vars: { groupJID: string; video?: boolean }) => {
      const { call } = await startGroupCall(sid, vars.groupJID, vars.video);
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
    onError: (e: Error) => toast.error(e.message),
  });
};
