import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { addParticipant, sendReaction, setHandRaised, startVideo, stopVideo } from "@/services/calls";
import { downgradeConnectionFromVideo, upgradeConnectionToVideo } from "@/stores/calls";

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

// useUpgradeToVideo faz uma chamada de áudio virar vídeo no meio da ligação:
// abre a câmera + o canal "vp8" via renegociação WebRTC
// (stores/calls.ts:upgradeConnectionToVideo), e só depois avisa o backend
// pra sinalizar o upgrade pro WhatsApp.
export const useUpgradeToVideo = () =>
  useMutation({
    mutationFn: async (vars: { sid: string; callId: string; camDeviceId: string | null }) => {
      await upgradeConnectionToVideo(vars.callId, vars.camDeviceId);
      await startVideo(vars.sid, vars.callId);
    },
    onError: (e: Error) => toast.error(e.message),
  });

export const useDowngradeFromVideo = () =>
  useMutation({
    mutationFn: async (vars: { sid: string; callId: string }) => {
      downgradeConnectionFromVideo(vars.callId);
      await stopVideo(vars.sid, vars.callId);
    },
    onError: (e: Error) => toast.error(e.message),
  });
