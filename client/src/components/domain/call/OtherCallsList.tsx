import { Phone } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/status-badge";
import { formatCallDuration } from "@/utils/format";
import { callStatusTone, callStatusPulse } from "@/lib/status";
import { useT } from "@/hooks/useT";
import type { CallSummary } from "@/types/call";

export const OtherCallsList = ({ calls }: { calls: CallSummary[] }) => {
  const t = useT();
  if (calls.length === 0) return null;
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-medium text-muted-foreground">
        {t.calls.otherActive}
      </h2>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {calls.map((c) => (
          <Card key={c.callId} className="opacity-90">
            <CardContent className="flex items-center gap-3 p-3">
              <span className="flex h-9 w-9 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <Phone className="h-4 w-4" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate font-mono text-sm font-medium">
                  {c.peer}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t.calls.direction[c.direction]}
                </p>
              </div>
              <StatusBadge
                tone={callStatusTone(c.status)}
                pulse={callStatusPulse(c.status)}
              >
                {c.status === "connected"
                  ? formatCallDuration(c.startedAt)
                  : t.calls.status[c.status]}
              </StatusBadge>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
};
