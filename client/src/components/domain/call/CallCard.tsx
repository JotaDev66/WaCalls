import { useEffect, useRef, useState } from "react";
import { Check, PhoneOff, WifiOff } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { attachMeter } from "@/lib/audio-meter";
import { useCalls } from "@/stores/calls";
import { useDevices } from "@/stores/devices";
import { useEndCall } from "@/hooks/useEndCall";
import { useT } from "@/hooks/useT";
import { callStatusTone, callStatusPulse } from "@/lib/status";
import { PeerAvatar } from "@/components/domain/contacts/PeerAvatar";
import { waveLevel } from "@/lib/waveform";
import { formatCallDuration } from "@/utils/format";
import type {
  CallStatus,
  CallSummary,
  QualitySample,
  SetupMark,
} from "@/types/call";

const waveBars = [
  { h: 8, delay: "0s" },
  { h: 14, delay: "0.12s" },
  { h: 20, delay: "0.24s" },
  { h: 13, delay: "0.12s" },
  { h: 9, delay: "0s" },
];

const Waveform = ({ db }: { db: number }) => (
  <div
    className="flex h-5 items-center gap-[3px]"
    style={{ transform: `scaleY(${waveLevel(db)})` }}
  >
    {waveBars.map((bar, i) => (
      <span
        key={i}
        className="tom-wave w-[2.5px] rounded-full bg-primary"
        style={{ height: `${bar.h}px`, animationDelay: bar.delay }}
      />
    ))}
  </div>
);

const Meter = ({ label, db }: { label: string; db: number }) => (
  <div className="space-y-1">
    <p className="text-xs text-muted-foreground">{label}</p>
    <Waveform db={db} />
  </div>
);

type QualityTone = "ok" | "warn" | "bad" | "idle";

const toneFill: Record<QualityTone, string> = {
  ok: "bg-primary",
  warn: "bg-amber-500",
  bad: "bg-destructive",
  idle: "bg-muted-foreground/40",
};

const toneText: Record<QualityTone, string> = {
  ok: "text-primary",
  warn: "text-amber-600 dark:text-amber-400",
  bad: "text-destructive",
  idle: "text-muted-foreground",
};

const clampPct = (value: number, full: number) =>
  Math.max(0, Math.min(100, (value / full) * 100));

const rttTone = (ms: number): QualityTone =>
  ms <= 150 ? "ok" : ms <= 300 ? "warn" : "bad";
const jitterTone = (ms: number): QualityTone =>
  ms <= 30 ? "ok" : ms <= 50 ? "warn" : "bad";
const lossTone = (frac: number): QualityTone =>
  frac <= 0.01 ? "ok" : frac <= 0.05 ? "warn" : "bad";

const QualityBar = ({
  label,
  value,
  pct,
  tone,
}: {
  label: string;
  value: string;
  pct: number;
  tone: QualityTone;
}) => (
  <div className="space-y-1">
    <div className="flex items-baseline justify-between">
      <span className="text-xs uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className={`font-mono text-xs font-semibold ${toneText[tone]}`}>
        {value}
      </span>
    </div>
    <div className="h-2 overflow-hidden rounded-full bg-muted">
      <div
        className={`h-full transition-all ${toneFill[tone]}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  </div>
);

const QualityPanel = ({ q }: { q: QualitySample | undefined }) => {
  const t = useT();
  if (!q) {
    return (
      <p className="text-xs text-muted-foreground">
        {t.calls.measuringQuality}
      </p>
    );
  }
  const lossPct = q.lossFraction * 100;
  return (
    <div className="space-y-2 rounded-md border border-border/60 p-2">
      {q.hasRtt ? (
        <QualityBar
          label={t.calls.rtt}
          value={`${Math.round(q.rttMs)} ms`}
          pct={clampPct(q.rttMs, 400)}
          tone={rttTone(q.rttMs)}
        />
      ) : (
        <QualityBar label={t.calls.rtt} value="—" pct={0} tone="idle" />
      )}
      <QualityBar
        label={t.calls.jitter}
        value={`${Math.round(q.jitterMs)} ms`}
        pct={clampPct(q.jitterMs, 80)}
        tone={jitterTone(q.jitterMs)}
      />
      <QualityBar
        label={t.calls.loss}
        value={`${lossPct.toFixed(1)}%`}
        pct={clampPct(lossPct, 10)}
        tone={lossTone(q.lossFraction)}
      />
    </div>
  );
};

const markSteps = [
  { key: "transport.ice", label: "ICE" },
  { key: "transport.dtls", label: "DTLS" },
  { key: "transport.sctp_open", label: "SCTP" },
  { key: "transport.stun", label: "STUN" },
  { key: "media.first_packet", label: "Media" },
];

const ConnectionTimeline = ({
  marks,
  status,
}: {
  marks: SetupMark[];
  status: CallStatus;
}) => {
  const [expanded, setExpanded] = useState(false);
  const t = useT();
  const byMark = new Map(marks.map((m) => [m.mark, m.elapsedMs]));
  const mediaMs = byMark.get("media.first_packet");

  if (status === "connected" && mediaMs !== undefined && !expanded) {
    return (
      <button
        type="button"
        onClick={() => setExpanded(true)}
        className="flex w-full items-center gap-2 rounded-md border border-border/60 px-2 py-1.5 text-xs text-muted-foreground"
      >
        <Check className="h-3.5 w-3.5 text-primary" />
        {t.calls.connectedIn} <span className="font-mono">{mediaMs} ms</span>
      </button>
    );
  }

  return (
    <div className="space-y-2 rounded-md border border-border/60 p-2">
      <div className="flex items-center justify-between">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          {t.calls.connection}
        </span>
        {mediaMs !== undefined && (
          <button
            type="button"
            onClick={() => setExpanded(false)}
            className="font-mono text-xs text-muted-foreground"
          >
            {mediaMs} ms
          </button>
        )}
      </div>
      <div className="grid grid-cols-5 gap-1">
        {markSteps.map((step) => {
          const ms = byMark.get(step.key);
          const reached = ms !== undefined;
          return (
            <div key={step.key} className="flex flex-col items-center gap-1">
              <div
                className={`h-2.5 w-2.5 rounded-full ${reached ? "bg-primary" : "bg-muted-foreground/40"}`}
              />
              <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                {step.label}
              </span>
              <span
                className={`font-mono text-[10px] ${reached ? "text-foreground" : "text-muted-foreground"}`}
              >
                {reached ? `${ms}` : "—"}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
};

const ReconnectingNotice = () => {
  const t = useT();
  const [showHint, setShowHint] = useState(false);
  return (
    <div className="space-y-1.5 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-1.5 text-xs text-amber-600 dark:text-amber-400">
      <div className="flex items-center gap-2">
        <WifiOff className="h-3.5 w-3.5" />
        {t.calls.reconnectingMedia}
      </div>
      <button
        type="button"
        onClick={() => setShowHint((v) => !v)}
        className="underline underline-offset-2"
      >
        {t.calls.reconnectWhy}
      </button>
      {showHint && <p className="leading-relaxed">{t.calls.reconnectHint}</p>}
    </div>
  );
};

export const CallCard = ({ call }: { call: CallSummary }) => {
  const conn = useCalls((s) => s.ownConnections.get(call.callId));
  const quality = useCalls((s) => s.quality.get(call.callId));
  const marks = useCalls((s) => s.marks.get(call.callId));
  const outDeviceId = useDevices((s) => s.outId);
  const endCall = useEndCall();
  const t = useT();
  const [, force] = useState(0);
  const [micDb, setMicDb] = useState(-60);
  const [peerDb, setPeerDb] = useState(-60);
  const audioRef = useRef<HTMLAudioElement>(null);

  useEffect(() => {
    const timer = setInterval(() => force((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!conn) return;
    const offMic = attachMeter(conn.micStream, setMicDb);
    let offPeer: (() => void) | null = null;
    const wait = setInterval(() => {
      if (conn.remoteStream && audioRef.current) {
        audioRef.current.srcObject = conn.remoteStream;
        audioRef.current.play().catch(() => {});
        offPeer = attachMeter(conn.remoteStream, setPeerDb);
        clearInterval(wait);
      }
    }, 200);
    return () => {
      offMic();
      offPeer?.();
      clearInterval(wait);
    };
  }, [conn]);

  useEffect(() => {
    const el = audioRef.current as
      (HTMLAudioElement & { setSinkId?: (id: string) => Promise<void> }) | null;
    if (!el || !outDeviceId || typeof el.setSinkId !== "function") return;
    el.setSinkId(outDeviceId).catch(() => {});
  }, [outDeviceId, conn]);

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <PeerAvatar
              name={call.peerName || call.peer}
              photoUrl={call.peerPhotoUrl}
            />
            <div className="min-w-0">
              <p className="truncate font-medium">
                {call.peerName || call.peer}
              </p>
              <StatusBadge
                tone={callStatusTone(call.status)}
                pulse={callStatusPulse(call.status)}
                className="mt-1"
              >
                {call.status === "connected"
                  ? formatCallDuration(call.startedAt)
                  : t.calls.status[call.status]}
              </StatusBadge>
            </div>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="destructive"
                size="icon"
                onClick={() =>
                  endCall.mutate({ sid: call.sessionId, callId: call.callId })
                }
                aria-label={t.calls.endCall}
              >
                <PhoneOff className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t.calls.endCall}</TooltipContent>
          </Tooltip>
        </div>
        {call.status === "reconnecting" && <ReconnectingNotice />}
        {marks && marks.length > 0 && (
          <ConnectionTimeline marks={marks} status={call.status} />
        )}
        <Meter label={t.calls.mic} db={micDb} />
        <Meter label={t.calls.peer} db={peerDb} />
        {call.status === "connected" && <QualityPanel q={quality} />}
        <audio ref={audioRef} autoPlay />
      </CardContent>
    </Card>
  );
};
