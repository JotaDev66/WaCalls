import { useState } from "react";
import { Download, History } from "lucide-react";
import { toast } from "sonner";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { EmptyState } from "@/components/shared/EmptyState";
import { useHistory } from "@/hooks/useHistory";
import { exportHistoryCsv } from "@/services/history";

export const HistoryDrawer = ({ sid }: { sid: string }) => {
  const [open, setOpen] = useState(false);
  const [exporting, setExporting] = useState(false);
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage } = useHistory(
    sid,
    open,
  );
  const rows = data?.pages.flatMap((p) => p.calls) ?? [];

  const onExport = async () => {
    setExporting(true);
    try {
      await exportHistoryCsv(sid);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setExporting(false);
    }
  };

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm">
          <History className="h-4 w-4" />
          History
        </Button>
      </SheetTrigger>
      <SheetContent side="right" className="w-full p-0 sm:max-w-md">
        <SheetHeader className="p-6 pb-4">
          <SheetTitle>Call history</SheetTitle>
        </SheetHeader>
        <Separator />
        <div className="flex justify-end px-6 pt-3">
          <Button
            variant="outline"
            size="sm"
            disabled={exporting || rows.length === 0}
            onClick={() => void onExport()}
          >
            <Download className="h-4 w-4" />
            Export CSV
          </Button>
        </div>
        <ScrollArea className="h-[calc(100vh-9rem)] px-6 py-4">
          {rows.length === 0 ? (
            <EmptyState
              title="No past calls"
              description="Calls you make or receive will appear here."
            />
          ) : (
            <>
              <ul className="space-y-2">
                {rows.map((r) => (
                  <li key={r.callId} className="rounded-lg border p-3">
                    <p className="font-mono font-medium">{r.peer}</p>
                    <p className="text-xs text-muted-foreground">
                      {r.direction} ·{" "}
                      <span className="font-mono">
                        {new Date(r.startedAt).toLocaleString()}
                      </span>
                    </p>
                  </li>
                ))}
              </ul>
              {hasNextPage && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="mt-3 w-full"
                  disabled={isFetchingNextPage}
                  onClick={() => void fetchNextPage()}
                >
                  {isFetchingNextPage ? "Loading…" : "Load more"}
                </Button>
              )}
            </>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
};
