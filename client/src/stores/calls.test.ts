import { describe, it, expect, beforeEach, vi } from "vitest";
import type { BrokerEvent } from "@/lib/event-stream";
import type { CallStatus } from "@/types/call";

const { listeners } = vi.hoisted(() => ({
  listeners: [] as Array<(ev: BrokerEvent) => void>,
}));

vi.mock("@/lib/event-stream", () => ({
  eventStream: {
    on: (l: (ev: BrokerEvent) => void) => {
      listeners.push(l);
      return () => {};
    },
  },
}));

vi.mock("@/lib/query", () => ({
  queryClient: { invalidateQueries: vi.fn() },
  queryKeys: { history: ["history"] },
}));

const { useCalls, ensureCallsWired } = await import("./calls");

const emit = (ev: BrokerEvent) => listeners.forEach((l) => l(ev));

const row = (callId: string, status: CallStatus = "connected") => ({
  sessionId: "s1",
  callId,
  owner: "op-A",
  direction: "outbound" as const,
  peer: "peer",
  startedAt: 1,
  status,
});

const sample = (id: string) => ({
  type: "call-quality" as const,
  sessionId: "s1",
  id,
  rttMs: 90,
  jitterMs: 12,
  lossFraction: 0.01,
  hasRtt: true,
});

const markEv = (id: string, mark: string, elapsedMs: number) => ({
  type: "call-mark" as const,
  sessionId: "s1",
  id,
  mark,
  elapsedMs,
});

const relayEv = (id: string, relayName: string, rttMs: number) => ({
  type: "call-relay" as const,
  sessionId: "s1",
  id,
  relayName,
  rttMs,
  hasRtt: true,
});

const peerMuteEv = (id: string, muted: boolean) => ({
  type: "call-peer-mute" as const,
  sessionId: "s1",
  id,
  muted,
});

ensureCallsWired();

describe("calls store event handlers", () => {
  beforeEach(() => {
    useCalls.setState({
      calls: [],
      ownConnections: new Map(),
      incoming: null,
      quality: new Map(),
      marks: new Map(),
      relays: new Map(),
      peerMuted: new Map(),
    });
  });

  it("call-list replaces the live calls and prunes orphaned quality entries", () => {
    useCalls.setState({
      quality: new Map([
        ["c1", sample("c1")],
        ["gone", sample("gone")],
      ]),
    });
    emit({ type: "call-list", calls: [row("c1"), row("c2")] });

    const st = useCalls.getState();
    expect(st.calls.map((c) => c.callId)).toEqual(["c1", "c2"]);
    expect([...st.quality.keys()]).toEqual(["c1"]); // "gone" pruned, "c2" not seeded
  });

  it("call-quality is tracked for a live call", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(sample("c1"));
    expect(useCalls.getState().quality.get("c1")?.rttMs).toBe(90);
  });

  it("ignores a call-quality sample for a call not in the live list", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(sample("ghost"));
    expect(useCalls.getState().quality.has("ghost")).toBe(false);
  });

  it("call-ended removes both the call and its quality entry", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(sample("c1"));
    emit({
      type: "call-ended",
      sessionId: "s1",
      id: "c1",
      owner: "op-A",
      reason: "user_ended",
      endedAt: 2,
    });

    const st = useCalls.getState();
    expect(st.calls).toHaveLength(0);
    expect(st.quality.has("c1")).toBe(false);
  });

  it("does not re-insert a straggler quality sample that races past call-ended", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(sample("c1"));
    emit({
      type: "call-ended",
      sessionId: "s1",
      id: "c1",
      owner: "op-A",
      reason: "user_ended",
      endedAt: 2,
    });
    emit(sample("c1")); // straggler arriving after the call already ended

    expect(useCalls.getState().quality.has("c1")).toBe(false);
  });

  it("call-mark accumulates setup marks for a live call in arrival order", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(markEv("c1", "transport.ice", 12));
    emit(markEv("c1", "transport.dtls", 45));
    const marks = useCalls.getState().marks.get("c1");
    expect(marks?.map((m) => m.mark)).toEqual([
      "transport.ice",
      "transport.dtls",
    ]);
    expect(marks?.[0].elapsedMs).toBe(12);
  });

  it("keeps the first occurrence of a repeated phase mark", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(markEv("c1", "transport.ice", 12));
    emit(markEv("c1", "transport.ice", 99)); // reconnect re-fires the phase
    const marks = useCalls.getState().marks.get("c1");
    expect(marks).toHaveLength(1);
    expect(marks?.[0].elapsedMs).toBe(12);
  });

  it("ignores a call-mark for a call not in the live list", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(markEv("ghost", "transport.ice", 12));
    expect(useCalls.getState().marks.has("ghost")).toBe(false);
  });

  it("tracks the relay sample for a live call and the latest event wins", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(relayEv("c1", "sfo1", 80));
    emit(relayEv("c1", "gru1", 24));
    expect(useCalls.getState().relays.get("c1")).toEqual({
      relayName: "gru1",
      rttMs: 24,
      hasRtt: true,
    });
  });

  it("ignores a relay sample for a call not in the live list", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(relayEv("ghost", "gru1", 24));
    expect(useCalls.getState().relays.has("ghost")).toBe(false);
  });

  it("call-list prunes orphaned relay samples and call-ended clears them", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(relayEv("c1", "gru1", 24));
    emit({ type: "call-list", calls: [] });
    expect(useCalls.getState().relays.size).toBe(0);

    emit({ type: "call-list", calls: [row("c2")] });
    emit(relayEv("c2", "gru1", 24));
    emit({
      type: "call-ended",
      sessionId: "s1",
      id: "c2",
      owner: "op-A",
      reason: "user_ended",
      endedAt: 2,
    });
    expect(useCalls.getState().relays.size).toBe(0);
  });

  it("tracks the peer mute state for a live call and the latest event wins", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(peerMuteEv("c1", true));
    expect(useCalls.getState().peerMuted.get("c1")).toBe(true);
    emit(peerMuteEv("c1", false));
    expect(useCalls.getState().peerMuted.get("c1")).toBe(false);
  });

  it("ignores a peer mute event for a call not in the live list", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(peerMuteEv("ghost", true));
    expect(useCalls.getState().peerMuted.has("ghost")).toBe(false);
  });

  it("call-list prunes orphaned peer mute entries and call-ended clears them", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(peerMuteEv("c1", true));
    emit({ type: "call-list", calls: [] });
    expect(useCalls.getState().peerMuted.size).toBe(0);

    emit({ type: "call-list", calls: [row("c2")] });
    emit(peerMuteEv("c2", true));
    emit({
      type: "call-ended",
      sessionId: "s1",
      id: "c2",
      owner: "op-A",
      reason: "user_ended",
      endedAt: 2,
    });
    expect(useCalls.getState().peerMuted.size).toBe(0);
  });

  it("call-list prunes orphaned marks and call-ended clears them", () => {
    emit({ type: "call-list", calls: [row("c1")] });
    emit(markEv("c1", "transport.ice", 12));
    emit({ type: "call-list", calls: [row("c2")] }); // c1 gone from the live set
    expect(useCalls.getState().marks.has("c1")).toBe(false);

    emit({ type: "call-list", calls: [row("c2")] });
    emit(markEv("c2", "transport.ice", 20));
    emit({
      type: "call-ended",
      sessionId: "s1",
      id: "c2",
      owner: "op-A",
      reason: "user_ended",
      endedAt: 3,
    });
    expect(useCalls.getState().marks.has("c2")).toBe(false);
  });
});
