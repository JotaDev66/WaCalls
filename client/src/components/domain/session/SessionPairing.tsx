import { useState } from "react";
import { Loader2 } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { toast } from "sonner";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { pairSession } from "@/services/sessions";
import { useSessions } from "@/stores/sessions";
import { useT } from "@/hooks/useT";
import type { SessionInfo } from "@/types/session";

export const SessionPairing = ({ session }: { session: SessionInfo }) => {
  const qr = useSessions((s) => s.qrs[session.id]);
  const [busy, setBusy] = useState(false);
  const t = useT();

  const steps = [t.pairing.step1, t.pairing.step2, t.pairing.step3];

  const reactivate = async () => {
    setBusy(true);
    try {
      await pairSession(session.id);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto max-w-5xl">
      <div className="grid gap-6 lg:grid-cols-[minmax(0,340px)_1fr]">
        <Card className="lg:self-start">
          <CardHeader>
            <CardTitle className="font-mono text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {t.pairing.stepsTitle}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <ol className="space-y-3">
              {steps.map((step, i) => (
                <li key={i} className="flex items-start gap-3">
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-primary/10 font-mono text-xs font-semibold text-primary">
                    {i + 1}
                  </span>
                  <span className="text-sm">{step}</span>
                </li>
              ))}
            </ol>
            <p className="text-xs text-muted-foreground">
              {t.pairing.autoRenews}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="dot-grid flex min-h-[340px] flex-col items-center justify-center gap-3 p-6">
            {qr ? (
              <div className="rounded-lg border bg-white p-3">
                <QRCodeSVG value={qr} size={232} marginSize={1} />
              </div>
            ) : session.state === "logged_out" ? (
              <>
                <p className="max-w-xs text-center text-sm text-muted-foreground">
                  {t.pairing.disconnectedBody}
                </p>
                <Button
                  size="sm"
                  disabled={busy}
                  onClick={() => void reactivate()}
                >
                  {busy && <Loader2 className="h-4 w-4 animate-spin" />}
                  {t.sessions.reactivate}
                </Button>
              </>
            ) : (
              <>
                <Skeleton className="h-[258px] w-[258px] rounded-lg" />
                <Badge variant="muted" className="gap-1.5">
                  <Loader2 className="h-3 w-3 animate-spin" />{" "}
                  {t.pairing.waitingQr}
                </Badge>
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
};
