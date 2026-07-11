import { create } from "zustand";

type State = { needsToken: boolean };

export const useAuth = create<State>(() => ({ needsToken: false }));

export const promptForToken = (): void => useAuth.setState({ needsToken: true });

export const clearTokenPrompt = (): void => useAuth.setState({ needsToken: false });
