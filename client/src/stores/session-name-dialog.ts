import { create } from "zustand";

type State = {
  open: boolean;
  mode: "create" | "rename";
  prefill: string;
  sessionId: string | null;
};

export const useSessionNameDialog = create<State>(() => ({
  open: false,
  mode: "create",
  prefill: "",
  sessionId: null,
}));

export const openCreateSession = (prefill = ""): void =>
  useSessionNameDialog.setState({
    open: true,
    mode: "create",
    prefill,
    sessionId: null,
  });

export const openRenameSession = (
  sessionId: string,
  currentName: string,
): void =>
  useSessionNameDialog.setState({
    open: true,
    mode: "rename",
    prefill: currentName,
    sessionId,
  });

export const closeSessionNameDialog = (): void =>
  useSessionNameDialog.setState({ open: false });
