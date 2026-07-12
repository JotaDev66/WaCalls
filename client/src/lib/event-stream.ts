import type { CallStatus } from "@/types/call";
import type { SessionInfo, SessionState } from "@/types/session";
import { getToken } from "./api-token";

type CallListRow = {
  sessionId: string;
  callId: string;
  owner: string | null;
  direction: "outbound" | "inbound";
  peer: string;
  startedAt: number;
  status: CallStatus;
  endedAt?: number;
  endReason?: string;
};

export type BrokerEvent =
  | { type: "session-list"; sessions: SessionInfo[] }
  | { type: "session-qr"; sessionId: string; qr: string }
  | {
      type: "auth-state";
      sessionId: string;
      paired: boolean;
      state: SessionState;
      qr?: string;
    }
  | { type: "call-list"; calls: CallListRow[] }
  | {
      type: "call-status";
      sessionId: string;
      id: string;
      owner: string | null;
      status: CallStatus;
      peer: string;
      startedAt: number;
    }
  | {
      type: "call-ended";
      sessionId: string;
      id: string;
      owner: string | null;
      reason: string;
      endedAt: number;
    }
  | {
      type: "incoming";
      sessionId: string;
      id: string;
      peer: string;
      offeredAt: number;
    }
  | { type: "incoming-claimed"; sessionId: string; id: string; owner: string };

type Listener = (ev: BrokerEvent) => void;
type StatusListener = (connected: boolean) => void;

const reconnectDelayMs = 3_000;
const livenessCheckMs = 10_000;
// The server emits a ping event every 10s; two missed pings mean the socket is dead
// even if the browser (or a proxy in between) still thinks it is open.
const staleAfterMs = 25_000;

class EventStream {
  #es: EventSource | null = null;
  #clientId = "";
  #listeners = new Set<Listener>();
  #statusListeners = new Set<StatusListener>();
  #retry: number | null = null;
  #watchdog: number | null = null;
  #lastActivity = 0;

  connect(clientId: string): void {
    this.#clientId = clientId;
    this.#open();
  }

  #open(): void {
    if (this.#es) return;
    const token = getToken();
    const auth = token ? `&access_token=${encodeURIComponent(token)}` : "";
    const es = new EventSource(
      `/api/events?clientId=${encodeURIComponent(this.#clientId)}${auth}`,
    );
    this.#es = es;
    this.#lastActivity = Date.now();
    es.onopen = () => {
      this.#lastActivity = Date.now();
      this.#emitStatus(true);
    };
    es.onmessage = (ev) => {
      this.#lastActivity = Date.now();
      try {
        const parsed: BrokerEvent | { type: "ping" } = JSON.parse(ev.data);
        if (parsed.type === "ping") return;
        for (const l of this.#listeners) l(parsed);
      } catch {}
    };
    es.onerror = () => {
      this.#emitStatus(false);
      // EventSource retries network failures on its own but gives up for good
      // on an HTTP error response (e.g. a 502 from a reverse proxy while the
      // backend restarts), so reconnection has to be handled here.
      if (es.readyState === EventSource.CLOSED) this.#scheduleReconnect();
    };
    this.#startWatchdog();
  }

  #scheduleReconnect(): void {
    this.#es?.close();
    this.#es = null;
    if (this.#retry !== null) return;
    this.#retry = window.setTimeout(() => {
      this.#retry = null;
      this.#open();
    }, reconnectDelayMs);
  }

  #startWatchdog(): void {
    if (this.#watchdog !== null) return;
    this.#watchdog = window.setInterval(() => {
      if (this.#es && Date.now() - this.#lastActivity > staleAfterMs) {
        this.#emitStatus(false);
        this.#scheduleReconnect();
      }
    }, livenessCheckMs);
  }

  #emitStatus(connected: boolean): void {
    for (const l of this.#statusListeners) l(connected);
  }

  on(l: Listener): () => void {
    this.#listeners.add(l);
    return () => this.#listeners.delete(l);
  }

  onStatus(l: StatusListener): () => void {
    this.#statusListeners.add(l);
    return () => this.#statusListeners.delete(l);
  }

  close(): void {
    if (this.#retry !== null) {
      window.clearTimeout(this.#retry);
      this.#retry = null;
    }
    if (this.#watchdog !== null) {
      window.clearInterval(this.#watchdog);
      this.#watchdog = null;
    }
    this.#es?.close();
    this.#es = null;
  }
}

export const eventStream = new EventStream();
