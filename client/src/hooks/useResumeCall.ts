import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { acquireMic, openCall } from "@/lib/webrtc";
import { micErrorMessage } from "@/lib/mic-error";
import { registerOwnConnection } from "@/stores/calls";
import { useT } from "@/hooks/useT";

// useResumeCall re-attaches the browser audio leg to a call that is still live on
// the server (held open by the grace window after a refresh). Unlike start/accept it
// never ends the call on failure: the call keeps running server-side and the operator
// can retry.
export const useResumeCall = (sid: string, micId: string | null) => {
  const t = useT();
  return useMutation({
    mutationFn: async (vars: { callId: string }) => {
      const mic = await acquireMic(micId);
      try {
        const conn = await openCall(sid, vars.callId, mic);
        registerOwnConnection(vars.callId, conn);
      } catch (err) {
        mic.getTracks().forEach((track) => track.stop());
        throw err;
      }
      return vars.callId;
    },
    onError: (e: Error) =>
      toast.error(micErrorMessage(e, t.calls) ?? e.message),
  });
};
