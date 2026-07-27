import { useState } from "react";
import {
  Loader2,
  MoreVertical,
  Pencil,
  Power,
  QrCode,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { StatusBadge } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { deleteSession, logoutSession, pairSession } from "@/services/sessions";
import { openRenameSession } from "@/stores/session-name-dialog";
import { useCalls } from "@/stores/calls";
import { useNav } from "@/stores/nav";
import { sessionStateTone, sessionStatePulse } from "@/lib/status";
import { initials } from "@/lib/initials";
import { cn } from "@/lib/utils";
import { useT } from "@/hooks/useT";
import type { SessionInfo } from "@/types/session";

export const SessionHeader = ({ session }: { session: SessionInfo }) => {
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const view = useNav((s) => s.view);
  const setView = useNav((s) => s.setView);
  const relay = useCalls((s) => {
    const live = s.calls.find(
      (c) =>
        c.sessionId === session.id &&
        c.status !== "ended" &&
        s.relays.has(c.callId),
    );
    return live ? s.relays.get(live.callId) : undefined;
  });
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

  const phone = session.jid ? session.jid.split("@")[0] : "";

  return (
    <div className="mx-auto max-w-5xl">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          {session.photoUrl ? (
            <img
              src={session.photoUrl}
              alt=""
              className="h-10 w-10 shrink-0 rounded-lg object-cover"
            />
          ) : (
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-sm font-semibold text-primary">
              {initials(session.name)}
            </span>
          )}
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="truncate text-xl font-semibold tracking-tight">
                {session.name}
              </h1>
              <StatusBadge
                tone={sessionStateTone(session.state)}
                pulse={sessionStatePulse(session.state)}
              >
                {t.sessions.status[session.state]}
              </StatusBadge>
              {relay && (
                <StatusBadge tone="ok">
                  relay {relay.relayName}
                  {relay.hasRtt && ` · ${relay.rttMs}ms`}
                </StatusBadge>
              )}
            </div>
            {phone && (
              <p className="truncate font-mono text-xs text-muted-foreground">
                {phone}
              </p>
            )}
          </div>
        </div>
        <div className="flex items-center gap-1">
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
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                aria-label={t.sessions.moreActions}
              >
                <MoreVertical className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() => openRenameSession(session.id, session.name)}
              >
                <Pencil className="h-4 w-4" />
                {t.sessions.rename}
              </DropdownMenuItem>
              <DropdownMenuItem
                className="text-destructive focus:text-destructive"
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2 className="h-4 w-4" />
                {t.common.delete}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      {session.paired && (
        <div className="mt-3 flex gap-1 border-b" role="tablist">
          {(["console", "contacts"] as const).map((v) => (
            <button
              key={v}
              type="button"
              role="tab"
              aria-selected={view === v}
              onClick={() => setView(v)}
              className={cn(
                "relative px-3 py-2 text-sm font-medium transition-colors",
                view === v
                  ? "text-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {v === "console" ? t.nav.console : t.nav.contacts}
              {view === v && (
                <span className="absolute inset-x-1 bottom-0 h-[2px] rounded-full bg-primary" />
              )}
            </button>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t.sessions.deleteTitle}
        description={t.sessions.deleteDescription(session.name)}
        confirmLabel={t.common.delete}
        destructive
        onConfirm={() =>
          void deleteSession(session.id).catch((e) =>
            toast.error((e as Error).message),
          )
        }
      />
    </div>
  );
};
