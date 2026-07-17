import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { acquireMic, openCall } from "@/lib/webrtc";
import { micErrorMessage } from "@/lib/mic-error";
import { acceptCall, endCall } from "@/services/calls";
import { registerOwnConnection, clearIncoming } from "@/stores/calls";
import { useT } from "@/hooks/useT";

export const useAcceptCall = (micId: string | null) => {
  const t = useT();
  return useMutation({
    mutationFn: async (vars: { sid: string; callId: string }) => {
      const mic = await acquireMic(micId);
      let callId: string;
      try {
        const res = await acceptCall(vars.sid, vars.callId);
        callId = res.call.callId;
      } catch (err) {
        mic.getTracks().forEach((track) => track.stop());
        throw err;
      }
      try {
        const conn = await openCall(vars.sid, callId, mic);
        registerOwnConnection(callId, conn);
      } catch (wrtcErr) {
        mic.getTracks().forEach((track) => track.stop());
        try {
          await endCall(vars.sid, callId);
        } catch {}
        throw wrtcErr;
      }
      clearIncoming();
      return callId;
    },
    onError: (e: Error) => {
      if (e.message.includes("409")) {
        clearIncoming();
        return;
      }
      toast.error(micErrorMessage(e, t.calls) ?? e.message);
    },
  });
};
