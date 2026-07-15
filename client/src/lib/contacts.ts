import type { Contact } from "@/types/contact";

export function filterContacts(query: string, contacts: Contact[]): Contact[] {
  const q = query.trim().toLowerCase();
  if (!q) return contacts;
  const digits = q.replace(/\D/g, "");
  return contacts.filter(
    (c) =>
      c.name.toLowerCase().includes(q) ||
      (digits !== "" && c.phone.includes(digits)),
  );
}

export function hasLetters(s: string): boolean {
  return /\p{L}/u.test(s);
}

export function contactInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "#";
  if (words.length === 1) return words[0][0].toUpperCase();
  return (words[0][0] + words[words.length - 1][0]).toUpperCase();
}

export const AVATAR_PALETTE_SIZE = 6;

export function avatarColorIndex(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) | 0;
  return Math.abs(h) % AVATAR_PALETTE_SIZE;
}
