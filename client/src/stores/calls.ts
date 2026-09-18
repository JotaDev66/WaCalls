import { create } from "zustand";
import { asCallMedia, eventStream, type BrokerEvent } from "@/lib/event-stream";
import { getClientId } from "@/lib/client-id";
import { queryClient, queryKeys } from "@/lib/query";
import type { OpenCall } from "@/lib/webrtc";
import type { CallSummary, IncomingPayload } from "@/types/call";

export type CallExtras = {
  lastReaction: { text: string; sender: string; at: number } | null;
  handRaised: boolean;
};

const emptyExtras: CallExtras = { lastReaction: null, handRaised: false };

type State = {
  calls: CallSummary[];
  ownConnections: Map<string, OpenCall>;
  incoming: IncomingPayload | null;
  extras: Map<string, CallExtras>;
};

export const useCalls = create<State>(() => ({
  calls: [],
  ownConnections: new Map(),
  incoming: null,
  extras: new Map(),
}));

export const callExtras = (id: string): CallExtras => useCalls.getState().extras.get(id) ?? emptyExtras;

let wired = false;
export const ensureCallsWired = (): void => {
  if (wired) return;
  wired = true;
  eventStream.on((ev: BrokerEvent) => {
    if (ev.type === "call-list") {
      useCalls.setState({ calls: ev.calls.map((c) => ({ ...c, media: asCallMedia(c.media) })) });
    } else if (ev.type === "call-status") {
      useCalls.setState((s) => ({
        calls: s.calls.map((c) =>
          c.callId === ev.id
            ? {
                ...c,
                sessionId: ev.sessionId,
                status: ev.status,
                peer: ev.peer,
                media: asCallMedia(ev.media),
                startedAt: ev.startedAt,
              }
            : c,
        ),
      }));
    } else if (ev.type === "call-ended") {
      useCalls.setState((s) => {
        const conn = s.ownConnections.get(ev.id);
        if (conn) conn.close();
        const nextConn = new Map(s.ownConnections);
        nextConn.delete(ev.id);
        const nextExtras = new Map(s.extras);
        nextExtras.delete(ev.id);
        return {
          calls: s.calls.filter((c) => c.callId !== ev.id),
          ownConnections: nextConn,
          extras: nextExtras,
          incoming: s.incoming?.callId === ev.id ? null : s.incoming,
        };
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.history });
    } else if (ev.type === "incoming") {
      useCalls.setState({
        incoming: {
          sessionId: ev.sessionId,
          callId: ev.id,
          peer: ev.peer,
          media: asCallMedia(ev.media),
          offeredAt: ev.offeredAt,
        },
      });
    } else if (ev.type === "incoming-claimed") {
      useCalls.setState((s) => (s.incoming?.callId === ev.id ? { incoming: null } : s));
    } else if (ev.type === "call-reaction") {
      useCalls.setState((s) => {
        const next = new Map(s.extras);
        const cur = next.get(ev.id) ?? emptyExtras;
        next.set(ev.id, { ...cur, lastReaction: { text: ev.text, sender: ev.sender, at: ev.at } });
        return { extras: next };
      });
    } else if (ev.type === "call-hand") {
      useCalls.setState((s) => {
        const next = new Map(s.extras);
        const cur = next.get(ev.id) ?? emptyExtras;
        next.set(ev.id, { ...cur, handRaised: ev.raised });
        return { extras: next };
      });
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
