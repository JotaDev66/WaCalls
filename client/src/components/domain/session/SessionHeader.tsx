import { useState } from "react";
import { Loader2, Power, QrCode } from "lucide-react";
import { toast } from "sonner";
import { StatusBadge } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import { logoutSession, pairSession } from "@/services/sessions";
import { sessionStateTone, sessionStatePulse } from "@/lib/status";
import { useT } from "@/hooks/useT";
import type { SessionInfo } from "@/types/session";

export const SessionHeader = ({ session }: { session: SessionInfo }) => {
  const [busy, setBusy] = useState(false);
  const t = useT();

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
    <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 items-center gap-2">
        <h1 className="truncate text-xl font-semibold tracking-tight">
          {session.name}
        </h1>
        <StatusBadge
          tone={sessionStateTone(session.state)}
          pulse={sessionStatePulse(session.state)}
        >
          {t.sessions.status[session.state]}
        </StatusBadge>
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
          {t.sessions.disconnect}
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
          {t.sessions.reactivate}
        </Button>
      )}
    </div>
  );
};
