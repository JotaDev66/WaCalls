import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { rejectCall } from "@/services/calls";
import { clearIncoming } from "@/stores/calls";

export const useRejectCall = () =>
  useMutation({
    mutationFn: (vars: { sid: string; callId: string }) =>
      rejectCall(vars.sid, vars.callId),
    onError: (e: Error) => toast.error(e.message),
    onSettled: () => clearIncoming(),
  });
