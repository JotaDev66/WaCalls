import { useState, type FormEvent } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { setActiveSession } from "@/stores/sessions";
import {
  closeSessionNameDialog,
  useSessionNameDialog,
} from "@/stores/session-name-dialog";
import { createSession, renameSession } from "@/services/sessions";
import { useT } from "@/hooks/useT";

type FormProps = {
  mode: "create" | "rename";
  prefill: string;
  sessionId: string | null;
};

const NameForm = ({ mode, prefill, sessionId }: FormProps) => {
  const [name, setName] = useState(prefill);
  const [busy, setBusy] = useState(false);
  const t = useT();

  const trimmed = name.trim();
  const submitDisabled =
    busy || (mode === "rename" && (trimmed === "" || trimmed === prefill));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (submitDisabled) return;
    setBusy(true);
    try {
      if (mode === "create") {
        const { id } = await createSession(trimmed || "WhatsApp");
        setActiveSession(id);
      } else if (sessionId) {
        await renameSession(sessionId, trimmed);
      }
      closeSessionNameDialog();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="space-y-2">
        <Label
          htmlFor="session-name"
          className="font-mono text-xs uppercase tracking-wide text-muted-foreground"
        >
          {t.sessions.nameLabel}
        </Label>
        <Input
          id="session-name"
          autoFocus
          value={name}
          placeholder="WhatsApp"
          onChange={(e) => setName(e.target.value)}
        />
      </div>
      <Button type="submit" className="w-full" disabled={submitDisabled}>
        {busy ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : mode === "create" ? (
          t.sessions.create
        ) : (
          t.sessions.save
        )}
      </Button>
    </form>
  );
};

export const SessionNameDialog = () => {
  const open = useSessionNameDialog((s) => s.open);
  const mode = useSessionNameDialog((s) => s.mode);
  const prefill = useSessionNameDialog((s) => s.prefill);
  const sessionId = useSessionNameDialog((s) => s.sessionId);
  const t = useT();

  return (
    <Dialog open={open} onOpenChange={(o) => !o && closeSessionNameDialog()}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {mode === "create"
              ? t.sessions.createTitle
              : t.sessions.renameTitle}
          </DialogTitle>
        </DialogHeader>
        <NameForm mode={mode} prefill={prefill} sessionId={sessionId} />
      </DialogContent>
    </Dialog>
  );
};
