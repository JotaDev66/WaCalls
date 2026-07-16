import { useMutation, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { fetchContacts, saveContact } from "@/services/contacts";
import type { SaveContactInput } from "@/types/contact";
import { queryClient } from "@/lib/query";
import { useT } from "@/hooks/useT";

export function useContacts(sid: string, enabled: boolean) {
  return useQuery({
    queryKey: ["contacts", sid],
    queryFn: () => fetchContacts(sid),
    enabled: enabled && !!sid,
    staleTime: 30_000,
  });
}

export function useSaveContact(sid: string) {
  const t = useT();
  return useMutation({
    mutationFn: (input: SaveContactInput) => saveContact(sid, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["contacts", sid] });
      toast.success(t.contacts.saveSuccess);
    },
  });
}
