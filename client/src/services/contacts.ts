import { apiGet } from "@/lib/api";
import type { Contact } from "@/types/contact";

export async function fetchContacts(sid: string): Promise<Contact[]> {
  const res = await apiGet<{ contacts: Contact[] }>(
    `/api/sessions/${sid}/contacts`,
  );
  return res.contacts;
}
