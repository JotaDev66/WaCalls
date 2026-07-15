import { useEffect, useState } from "react";
import { PhoneCall } from "lucide-react";
import { Dialer } from "@/components/domain/call/Dialer";
import { CallCard } from "@/components/domain/call/CallCard";
import { OtherCallsList } from "@/components/domain/call/OtherCallsList";
import { HistoryDrawer } from "@/components/domain/history/HistoryDrawer";
import { EmptyState } from "@/components/shared/EmptyState";
import { isMine, useCalls } from "@/stores/calls";
import { useT } from "@/hooks/useT";

export const CallsPage = ({ sid }: { sid: string }) => {
  const calls = useCalls((s) => s.calls);
  const [, force] = useState(0);
  const t = useT();

  useEffect(() => {
    const timer = setInterval(() => force((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  const sessionCalls = calls.filter(
    (c) => c.sessionId === sid && c.status !== "ended",
  );
  const mine = sessionCalls.filter(isMine);
  const others = sessionCalls.filter((c) => !isMine(c));

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium text-muted-foreground">
          <span className="font-mono">{mine.length}</span>{" "}
          {t.calls.activeLabel(mine.length)}
        </h2>
        <HistoryDrawer sid={sid} />
      </div>
      <Dialer sid={sid} />
      {mine.length > 0 ? (
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          {mine.map((c) => (
            <CallCard key={c.callId} call={c} />
          ))}
        </div>
      ) : (
        <EmptyState
          icon={<PhoneCall className="h-6 w-6" />}
          title={t.calls.noCallsTitle}
          description={t.calls.noCallsDescription}
        />
      )}
      <OtherCallsList calls={others} />
    </div>
  );
};
