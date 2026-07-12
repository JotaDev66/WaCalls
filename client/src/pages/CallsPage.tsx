import { useEffect, useState } from "react";
import { PhoneCall } from "lucide-react";
import { Dialer } from "@/components/domain/call/Dialer";
import { CallCard } from "@/components/domain/call/CallCard";
import { OtherCallsList } from "@/components/domain/call/OtherCallsList";
import { HistoryDrawer } from "@/components/domain/history/HistoryDrawer";
import { EmptyState } from "@/components/shared/EmptyState";
import { isMine, useCalls } from "@/stores/calls";

export const CallsPage = ({ sid }: { sid: string }) => {
  const calls = useCalls((s) => s.calls);
  const [, force] = useState(0);

  useEffect(() => {
    const t = setInterval(() => force((n) => n + 1), 1000);
    return () => clearInterval(t);
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
          {mine.length} active call{mine.length === 1 ? "" : "s"}
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
          title="No active calls"
          description="Dial a number above to start a call."
        />
      )}
      <OtherCallsList calls={others} />
    </div>
  );
};
