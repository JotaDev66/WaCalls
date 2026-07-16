import { create } from "zustand";
import { eventStream } from "@/lib/event-stream";

type State = { connected: boolean };

export const useConnection = create<State>(() => ({ connected: true }));

let wired = false;
export const ensureConnectionWired = (): void => {
  if (wired) return;
  wired = true;
  eventStream.onStatus((connected) => useConnection.setState({ connected }));
};
