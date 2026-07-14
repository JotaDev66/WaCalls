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

ensureCallsWired();

describe("calls store event handlers", () => {
  beforeEach(() => {
    useCalls.setState({
      calls: [],
      ownConnections: new Map(),
      incoming: null,
      quality: new Map(),
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
});
