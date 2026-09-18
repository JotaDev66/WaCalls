import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { addParticipant, sendReaction, setHandRaised } from "@/services/calls";

export const useSendReaction = () =>
  useMutation({
    mutationFn: (vars: { sid: string; callId: string; text: string }) =>
      sendReaction(vars.sid, vars.callId, vars.text),
    onError: (e: Error) => toast.error(e.message),
  });

export const useSetHandRaised = () =>
  useMutation({
    mutationFn: (vars: { sid: string; callId: string; raised: boolean }) =>
      setHandRaised(vars.sid, vars.callId, vars.raised),
    onError: (e: Error) => toast.error(e.message),
  });

export const useAddParticipant = () =>
  useMutation({
    mutationFn: (vars: { sid: string; callId: string; target: string }) =>
      addParticipant(vars.sid, vars.callId, vars.target),
    onError: (e: Error) => toast.error(e.message),
  });
