import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { endCall } from "@/services/calls";

export const useEndCall = () =>
  useMutation({
    mutationFn: (vars: { sid: string; callId: string }) =>
      endCall(vars.sid, vars.callId),
    onError: (e: Error) => toast.error(e.message),
  });
