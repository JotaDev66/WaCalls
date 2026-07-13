import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchHistory } from "@/services/history";

export const useHistory = (sid: string | null, enabled: boolean) =>
  useInfiniteQuery({
    queryKey: ["history", sid],
    queryFn: ({ pageParam }) => fetchHistory(sid as string, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor,
    enabled: enabled && !!sid,
  });
