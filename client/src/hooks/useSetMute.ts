import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { setMute } from "@/services/calls";

export const useSetMute = () =>
  useMutation({
    mutationFn: (vars: { sid: string; callId: string; muted: boolean }) =>
      setMute(vars.sid, vars.callId, vars.muted),
    onError: (e: Error) => toast.error(e.message),
  });
