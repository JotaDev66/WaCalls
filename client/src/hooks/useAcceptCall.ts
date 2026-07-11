import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { openCall } from "@/lib/webrtc";
import { acceptCall, endCall } from "@/services/calls";
import { registerOwnConnection, clearIncoming } from "@/stores/calls";

export const useAcceptCall = (micId: string | null) =>
  useMutation({
    mutationFn: async (vars: { sid: string; callId: string }) => {
      const res = await acceptCall(vars.sid, vars.callId);
      try {
        const conn = await openCall(vars.sid, res.call.callId, micId);
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
