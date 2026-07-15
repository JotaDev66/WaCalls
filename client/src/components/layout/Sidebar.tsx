import { useState } from "react";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { setActiveSession, useSessions } from "@/stores/sessions";
import { createSession, deleteSession } from "@/services/sessions";
import { useT } from "@/hooks/useT";
import type { SessionInfo, SessionState } from "@/types/session";

const dotClass: Record<SessionState, string> = {
  open: "bg-primary",
  qr: "bg-amber-500",
  connecting: "bg-muted-foreground/50",
  logged_out: "bg-destructive",
};

export const Sidebar = ({ onNavigate }: { onNavigate?: () => void }) => {
  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  const [creating, setCreating] = useState(false);
  const [toDelete, setToDelete] = useState<SessionInfo | null>(null);
  const t = useT();

  const onNew = async () => {
    setCreating(true);
    try {
      const { id } = await createSession("WhatsApp");
      setActiveSession(id);
      onNavigate?.();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setCreating(false);
    }
  };

  const remove = async (id: string) => {
    try {
      await deleteSession(id);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  return (
    <div className="flex h-full flex-col gap-2 p-3">
      <p className="px-2 pt-1 font-mono text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {t.sessions.accounts}
      </p>
      <div className="flex-1 space-y-1 overflow-y-auto">
        {sessions.map((s) => (
          <div
            key={s.id}
            role="button"
            tabIndex={0}
            onClick={() => {
              setActiveSession(s.id);
              onNavigate?.();
            }}
            className={cn(
              "group relative flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-sm",
              s.id === activeId
                ? "bg-accent text-accent-foreground"
                : "hover:bg-muted",
            )}
          >
            {s.id === activeId && (
              <span className="absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-full bg-primary" />
            )}
            <span
              className={cn("h-2 w-2 shrink-0 rounded-full", dotClass[s.state])}
            />
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium">{s.name}</p>
              {s.jid && (
                <p className="truncate font-mono text-xs text-muted-foreground">
                  {s.jid.split("@")[0]}
                </p>
              )}
            </div>
            <button
              onClick={(e) => {
                e.stopPropagation();
                setToDelete(s);
              }}
              className="text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
              aria-label={t.sessions.deleteAria(s.name)}
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
        ))}
        {sessions.length === 0 && (
          <p className="px-2 text-sm text-muted-foreground">
            {t.sessions.noAccounts}
          </p>
        )}
      </div>
      <Button
        variant="outline"
        className="w-full"
        onClick={onNew}
        disabled={creating}
      >
        {creating ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <Plus className="h-4 w-4" />
        )}
        {t.sessions.newSession}
      </Button>

      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={t.sessions.deleteTitle}
        description={
          toDelete ? t.sessions.deleteDescription(toDelete.name) : undefined
        }
        confirmLabel={t.common.delete}
        destructive
        onConfirm={() => {
          if (toDelete) void remove(toDelete.id);
        }}
      />
    </div>
  );
};
