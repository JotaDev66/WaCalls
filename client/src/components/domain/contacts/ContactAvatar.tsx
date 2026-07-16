import { contactInitials, avatarColorIndex } from "@/lib/contacts";

const PALETTE = [
  "bg-primary/15 text-primary",
  "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  "bg-sky-500/15 text-sky-600 dark:text-sky-400",
  "bg-indigo-500/15 text-indigo-600 dark:text-indigo-400",
  "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  "bg-rose-500/15 text-rose-600 dark:text-rose-400",
];

export function ContactAvatar({ name }: { name: string }) {
  const cls = PALETTE[avatarColorIndex(name)];
  return (
    <span
      aria-hidden
      className={`flex size-9 shrink-0 items-center justify-center rounded-full text-xs font-medium ${cls}`}
    >
      {contactInitials(name)}
    </span>
  );
}
