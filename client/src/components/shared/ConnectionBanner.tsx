import { WifiOff } from "lucide-react";
import { useConnection } from "@/stores/connection";
import { useT } from "@/hooks/useT";

export const ConnectionBanner = () => {
  const connected = useConnection((s) => s.connected);
  const t = useT();
  if (connected) return null;
  return (
    <div className="flex items-center justify-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-4 py-1.5 text-sm text-amber-600 dark:text-amber-400">
      <WifiOff className="h-3.5 w-3.5" />
      {t.connection.reconnecting}
    </div>
  );
};
