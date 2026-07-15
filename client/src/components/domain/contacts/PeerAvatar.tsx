import { useEffect, useState } from "react";
import { Phone } from "lucide-react";
import { ContactAvatar } from "./ContactAvatar";
import { hasLetters } from "@/lib/contacts";

export function PeerAvatar({
  name,
  photoUrl,
}: {
  name: string;
  photoUrl?: string;
}) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [photoUrl]);

  if (photoUrl && !failed) {
    return (
      <img
        src={photoUrl}
        alt=""
        onError={() => setFailed(true)}
        className="size-9 shrink-0 rounded-full object-cover"
      />
    );
  }
  if (hasLetters(name)) return <ContactAvatar name={name} />;
  return (
    <span
      aria-hidden
      className="flex size-9 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground"
    >
      <Phone className="h-4 w-4" />
    </span>
  );
}
