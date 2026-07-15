import { useQuery } from "@tanstack/react-query";
import { fetchContacts } from "@/services/contacts";

export function useContacts(sid: string, enabled: boolean) {
  return useQuery({
    queryKey: ["contacts", sid],
    queryFn: () => fetchContacts(sid),
    enabled: enabled && !!sid,
    staleTime: 30_000,
  });
}
