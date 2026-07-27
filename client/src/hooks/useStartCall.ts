import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { acquireMic, openCall } from "@/lib/webrtc";
import { micErrorMessage } from "@/lib/mic-error";
import { startCall, endCall } from "@/services/calls";
import { registerOwnConnection } from "@/stores/calls";
import { useT } from "@/hooks/useT";

export const useStartCall = (sid: string, micId: string | null) => {
  const t = useT();
  return useMutation({
    mutationFn: async (vars: { phone: string }) => {
      const mic = await acquireMic(micId);
      let callId: string;
      try {
        const { call } = await startCall(sid, vars.phone);
        callId = call.callId;
      } catch (err) {
        mic.getTracks().forEach((track) => track.stop());
        throw err;
      }
      try {
        const conn = await openCall(sid, callId, mic);
        registerOwnConnection(callId, conn);
      } catch (err) {
        mic.getTracks().forEach((track) => track.stop());
        try {
          await endCall(sid, callId);
        } catch {}
        throw err;
      }
      return callId;
    },
    onError: (e: Error) => {
      const micMsg = micErrorMessage(e, t.calls);
      if (micMsg) toast.error(micMsg);
      else if (e.message.includes("429"))
        toast.error("Limit reached: max concurrent calls.");
      else if (e.message.includes("503")) toast.error("WhatsApp not paired.");
      else toast.error(e.message);
    },
  });
};
