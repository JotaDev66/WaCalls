import { useState } from "react";
import { Loader2, Power, QrCode } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { logoutSession, pairSession } from "@/services/sessions";
import type { SessionInfo, SessionState } from "@/types/session";

const statusLabel: Record<SessionState, string> = {
  open: "Connected",
  qr: "Scan QR",
  connecting: "Connecting…",
  logged_out: "Disconnected",
};

const statusVariant: Record<
  SessionState,
  "success" | "secondary" | "muted" | "destructive"
> = {
  open: "success",
  qr: "secondary",
  connecting: "muted",
  logged_out: "destructive",
};

export const SessionHeader = ({ session }: { session: SessionInfo }) => {
  const [busy, setBusy] = useState(false);

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex max-w-3xl flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 items-center gap-2">
        <h1 className="truncate text-xl font-semibold tracking-tight">
          {session.name}
        </h1>
        <Badge variant={statusVariant[session.state]}>
          {statusLabel[session.state]}
        </Badge>
      </div>
      {session.paired ? (
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => run(() => logoutSession(session.id))}
        >
          {busy ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Power className="h-4 w-4" />
          )}
          Disconnect
        </Button>
      ) : (
        <Button
          size="sm"
          disabled={busy}
          onClick={() => run(() => pairSession(session.id))}
        >
          {busy ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <QrCode className="h-4 w-4" />
          )}
          Reactivate
        </Button>
      )}
    </div>
  );
};
