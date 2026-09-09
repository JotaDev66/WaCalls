import { create } from "zustand";

type State = {
  micId: string | null;
  outId: string | null;
  camId: string | null;
  setMic: (id: string) => void;
  setOut: (id: string) => void;
  setCam: (id: string) => void;
};

export const useDevices = create<State>((set) => ({
  micId: null,
  outId: null,
  camId: null,
  setMic: (id) => set({ micId: id || null }),
  setOut: (id) => set({ outId: id || null }),
  setCam: (id) => set({ camId: id || null }),
}));
