import { useEffect, useRef, useState } from "react";
import { Hand, PhoneOff, Send, UserPlus, Video, VideoOff } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { attachMeter } from "@/lib/audio-meter";
import { videoCallSupported } from "@/lib/video-pipe";
import { useCalls } from "@/stores/calls";
import { useDevices } from "@/stores/devices";
import { useEndCall } from "@/hooks/useEndCall";
import {
  useAddParticipant,
  useDowngradeFromVideo,
  useSendReaction,
  useSetHandRaised,
  useUpgradeToVideo,
} from "@/hooks/useCallActions";
import { formatCallDuration } from "@/utils/format";
import type { CallStatus, CallSummary } from "@/types/call";

const QUICK_EMOJI = ["👍", "❤️", "😂", "😮", "👏"];
const canVideo = videoCallSupported();

const statusVariant: Record<CallStatus, "success" | "secondary" | "muted"> = {
  connected: "success",
  ringing: "secondary",
  starting: "secondary",
  ended: "muted",
};

const Meter = ({ label, db }: { label: string; db: number }) => {
  const pct = Math.max(0, Math.min(100, Math.round(((db + 60) / 60) * 100)));
  return (
    <div className="space-y-1">
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className="h-2 overflow-hidden rounded-full bg-muted">
        <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
};

export const CallCard = ({ call }: { call: CallSummary }) => {
  const conn = useCalls((s) => s.ownConnections.get(call.callId));
  const extras = useCalls((s) => s.extras.get(call.callId));
  const outDeviceId = useDevices((s) => s.outId);
  const camDeviceId = useDevices((s) => s.camId);
  const endCall = useEndCall();
  const sendReaction = useSendReaction();
  const setHandRaised = useSetHandRaised();
  const addParticipant = useAddParticipant();
  const upgradeToVideo = useUpgradeToVideo();
  const downgradeFromVideo = useDowngradeFromVideo();
  const [, force] = useState(0);
  const [micDb, setMicDb] = useState(-60);
  const [peerDb, setPeerDb] = useState(-60);
  const [reactionText, setReactionText] = useState("");
  const [participantInput, setParticipantInput] = useState("");
  const audioRef = useRef<HTMLAudioElement>(null);
  const remoteVideoRef = useRef<HTMLVideoElement>(null);
  const localVideoRef = useRef<HTMLVideoElement>(null);
  const isVideo = call.media === "video";
  const handRaised = extras?.handRaised ?? false;
  const recentReaction =
    extras?.lastReaction && Date.now() - extras.lastReaction.at < 5000 ? extras.lastReaction : null;

  useEffect(() => {
    const t = setInterval(() => force((n) => n + 1), 1000);
    return () => clearInterval(t);
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
    if (!conn) return;
    if (remoteVideoRef.current && conn.remoteVideoStream) {
      remoteVideoRef.current.srcObject = conn.remoteVideoStream;
      remoteVideoRef.current.play().catch(() => {});
    }
    if (localVideoRef.current && conn.localVideoStream) {
      localVideoRef.current.srcObject = conn.localVideoStream;
      localVideoRef.current.play().catch(() => {});
    }
  }, [conn]);

  useEffect(() => {
    const el = audioRef.current as (HTMLAudioElement & { setSinkId?: (id: string) => Promise<void> }) | null;
    if (!el || !outDeviceId || typeof el.setSinkId !== "function") return;
    el.setSinkId(outDeviceId).catch(() => {});
  }, [outDeviceId, conn]);

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="truncate font-medium">{call.peer}</p>
            <Badge variant={statusVariant[call.status]} className="mt-1">
              {formatCallDuration(call.startedAt, call.status)}
            </Badge>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="destructive"
                size="icon"
                onClick={() => endCall.mutate({ sid: call.sessionId, callId: call.callId })}
                aria-label="End call"
              >
                <PhoneOff className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>End call</TooltipContent>
          </Tooltip>
        </div>
        {isVideo && (
          <div className="relative overflow-hidden rounded-md bg-black">
            <video
              ref={remoteVideoRef}
              className="aspect-video w-full object-cover"
              autoPlay
              playsInline
            />
            <video
              ref={localVideoRef}
              className="absolute bottom-2 right-2 w-1/3 max-w-[140px] -scale-x-100 rounded border border-white/20 bg-black object-cover"
              autoPlay
              playsInline
              muted
            />
            {recentReaction && (
              <div className="absolute left-2 top-2 rounded-full bg-black/60 px-2 py-1 text-lg">
                {recentReaction.text}
              </div>
            )}
          </div>
        )}
        <Meter label="Mic" db={micDb} />
        <Meter label="Peer" db={peerDb} />
        <audio ref={audioRef} autoPlay />

        <div className="flex flex-wrap items-center gap-1.5 border-t pt-3">
          {canVideo && conn && call.status === "connected" && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={isVideo ? "default" : "outline"}
                  size="icon"
                  disabled={upgradeToVideo.isPending || downgradeFromVideo.isPending}
                  onClick={() =>
                    isVideo
                      ? downgradeFromVideo.mutate({ sid: call.sessionId, callId: call.callId })
                      : upgradeToVideo.mutate({ sid: call.sessionId, callId: call.callId, camDeviceId })
                  }
                  aria-label={isVideo ? "Switch to audio" : "Switch to video"}
                >
                  {isVideo ? <VideoOff className="h-4 w-4" /> : <Video className="h-4 w-4" />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{isVideo ? "Switch to audio" : "Switch to video"}</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant={handRaised ? "default" : "outline"}
                size="icon"
                onClick={() =>
                  setHandRaised.mutate({ sid: call.sessionId, callId: call.callId, raised: !handRaised })
                }
                aria-label="Raise hand"
              >
                <Hand className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{handRaised ? "Lower hand" : "Raise hand"}</TooltipContent>
          </Tooltip>
          {QUICK_EMOJI.map((e) => (
            <Button
              key={e}
              variant="outline"
              size="icon"
              onClick={() => sendReaction.mutate({ sid: call.sessionId, callId: call.callId, text: e })}
              aria-label={`React ${e}`}
            >
              {e}
            </Button>
          ))}
          <form
            className="flex flex-1 min-w-[140px] gap-1.5"
            onSubmit={(e) => {
              e.preventDefault();
              const text = reactionText.trim();
              if (!text) return;
              sendReaction.mutate({ sid: call.sessionId, callId: call.callId, text });
              setReactionText("");
            }}
          >
            <Input
              value={reactionText}
              onChange={(e) => setReactionText(e.target.value)}
              placeholder="Reaction text"
              className="h-9"
            />
            <Button type="submit" variant="outline" size="icon" aria-label="Send reaction">
              <Send className="h-4 w-4" />
            </Button>
          </form>
        </div>

        <form
          className="flex gap-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            const target = participantInput.trim();
            if (!target) return;
            addParticipant.mutate({ sid: call.sessionId, callId: call.callId, target });
            setParticipantInput("");
          }}
        >
          <Input
            value={participantInput}
            onChange={(e) => setParticipantInput(e.target.value)}
            placeholder="Add participant (phone number)"
            className="h-9"
          />
          <Button type="submit" variant="outline" size="icon" aria-label="Add participant">
            <UserPlus className="h-4 w-4" />
          </Button>
        </form>
      </CardContent>
    </Card>
  );
};
