import { apiGet, apiGetBlob } from "@/lib/api";
import type { HistoryPage } from "@/types/history";

export const fetchHistory = (sid: string, cursor?: string) => {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor) params.set("cursor", cursor);
  return apiGet<HistoryPage>(
    `/api/sessions/${sid}/history?${params.toString()}`,
  );
};

export const exportHistoryCsv = async (sid: string) => {
  const blob = await apiGetBlob(`/api/sessions/${sid}/history/export`);
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `history-${sid}.csv`;
  a.click();
  URL.revokeObjectURL(url);
};
