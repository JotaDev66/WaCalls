import { apiGet, apiPost } from "@/lib/api";
import type { Contact, SaveContactInput } from "@/types/contact";

export async function fetchContacts(sid: string): Promise<Contact[]> {
  const res = await apiGet<{ contacts: Contact[] }>(
    `/api/sessions/${sid}/contacts`,
  );
  return res.contacts;
}

export async function saveContact(
  sid: string,
  input: SaveContactInput,
): Promise<Contact> {
  const res = await apiPost<{ contact: Contact }>(
    `/api/sessions/${sid}/contacts`,
    input,
  );
  return res.contact;
}
