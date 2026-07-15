import { useMemo, useState } from "react";
import { Phone, RefreshCw, Search } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { PeerAvatar } from "@/components/domain/contacts/PeerAvatar";
import { useContacts } from "@/hooks/useContacts";
import { filterContacts } from "@/lib/contacts";
import { useStartCall } from "@/hooks/useStartCall";
import { useDevices } from "@/stores/devices";
import { useNav } from "@/stores/nav";
import { useT } from "@/hooks/useT";

export const ContactsPage = ({ sid }: { sid: string }) => {
  const t = useT();
  const micId = useDevices((s) => s.micId);
  const setView = useNav((s) => s.setView);
  const startCall = useStartCall(sid, micId);

  const call = (phone: string) => {
    startCall.mutate({ phone });
    setView("console");
  };
  const { data, isLoading, isError, isFetching, refetch } = useContacts(
    sid,
    true,
  );
  const [q, setQ] = useState("");
  const list = useMemo(() => filterContacts(q, data ?? []), [q, data]);

  return (
    <Card>
      <CardHeader className="space-y-3">
        <div className="flex items-center justify-between">
          <CardTitle>{t.contacts.title}</CardTitle>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => refetch()}
            disabled={isFetching}
          >
            <RefreshCw
              className={`h-4 w-4 ${isFetching ? "animate-spin" : ""}`}
            />
            {t.contacts.refresh}
          </Button>
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t.contacts.searchPlaceholder}
            className="pl-9"
          />
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 py-1.5">
                <Skeleton className="size-9 rounded-full" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-32" />
                  <Skeleton className="h-3 w-24" />
                </div>
              </div>
            ))}
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center gap-2 py-10 text-center text-sm text-muted-foreground">
            <p>{t.contacts.error}</p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              {t.contacts.retry}
            </Button>
          </div>
        ) : (data ?? []).length === 0 ? (
          <div className="flex flex-col items-center gap-1 py-10 text-center">
            <p className="text-sm font-medium">{t.contacts.empty}</p>
            <p className="max-w-xs text-xs text-muted-foreground">
              {t.contacts.emptyHint}
            </p>
          </div>
        ) : list.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">
            {t.contacts.noResults}
          </p>
        ) : (
          <ScrollArea className="h-[28rem] pr-3">
            <ul className="space-y-1">
              {list.map((c) => (
                <li
                  key={c.jid}
                  className="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-muted/50"
                >
                  <PeerAvatar name={c.name} photoUrl={c.photoUrl} />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{c.name}</p>
                    <p className="truncate font-mono text-xs text-muted-foreground">
                      {c.phone}
                    </p>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    onClick={() => call(c.phone)}
                    disabled={startCall.isPending}
                    aria-label={t.contacts.callAria(c.name)}
                  >
                    <Phone className="h-4 w-4" />
                    {t.contacts.call}
                  </Button>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  );
};
