import { create } from "zustand";
import { eventStream, type BrokerEvent } from "@/lib/event-stream";
import { getClientId } from "@/lib/client-id";
import { queryClient, queryKeys } from "@/lib/query";
import type { OpenCall } from "@/lib/webrtc";
import type { CallSummary, IncomingPayload } from "@/types/call";

type State = {
  calls: CallSummary[];
  ownConnections: Map<string, OpenCall>;
  incoming: IncomingPayload | null;
};

export const useCalls = create<State>(() => ({
  calls: [],
  ownConnections: new Map(),
  incoming: null,
}));

let wired = false;
export const ensureCallsWired = (): void => {
  if (wired) return;
  wired = true;
  eventStream.on((ev: BrokerEvent) => {
    if (ev.type === "call-list") {
      useCalls.setState({ calls: ev.calls });
    } else if (ev.type === "call-status") {
      useCalls.setState((s) => ({
        calls: s.calls.map((c) =>
          c.callId === ev.id
            ? { ...c, sessionId: ev.sessionId, status: ev.status, peer: ev.peer, startedAt: ev.startedAt }
            : c,
        ),
      }));
    } else if (ev.type === "call-ended") {
      useCalls.setState((s) => {
        const conn = s.ownConnections.get(ev.id);
        if (conn) conn.close();
        const next = new Map(s.ownConnections);
        next.delete(ev.id);
        return {
          calls: s.calls.filter((c) => c.callId !== ev.id),
          ownConnections: next,
          incoming: s.incoming?.callId === ev.id ? null : s.incoming,
        };
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.history });
    } else if (ev.type === "incoming") {
      useCalls.setState({ incoming: { sessionId: ev.sessionId, callId: ev.id, peer: ev.peer, offeredAt: ev.offeredAt } });
    } else if (ev.type === "incoming-claimed") {
      useCalls.setState((s) => (s.incoming?.callId === ev.id ? { incoming: null } : s));
    }
  });
};

export const isMine = (call: CallSummary): boolean => call.owner === getClientId();

export const registerOwnConnection = (id: string, conn: OpenCall): void => {
  useCalls.setState((s) => {
    const next = new Map(s.ownConnections);
    next.set(id, conn);
    return { ownConnections: next };
  });
};

export const clearIncoming = (): void => useCalls.setState({ incoming: null });
