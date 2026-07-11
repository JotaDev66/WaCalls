import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { openCall } from "@/lib/webrtc";
import { startCall } from "@/services/calls";
import { registerOwnConnection } from "@/stores/calls";

export const useStartCall = (sid: string, micId: string | null) =>
  useMutation({
    mutationFn: async (vars: { phone: string; record: boolean }) => {
      const { call } = await startCall(sid, vars.phone, vars.record);
      const conn = await openCall(sid, call.callId, micId);
      registerOwnConnection(call.callId, conn);
      return call.callId;
    },
    onError: (e: Error) => {
      const m = e.message;
      if (m.includes("429")) toast.error("Limit reached: max concurrent calls.");
      else if (m.includes("503")) toast.error("WhatsApp not paired.");
      else toast.error(m);
    },
  });
