import { apiGet } from "@/lib/api";
import type { HistoryRow } from "@/types/history";

export const fetchHistory = (sid: string) =>
  apiGet<{ rows: HistoryRow[] }>(`/api/sessions/${sid}/history?limit=50`).then(
    (r) => r.rows ?? [],
  );
