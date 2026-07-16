import { useState } from "react";
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
  // Track the URL that failed rather than a boolean so a new photoUrl retries
  // on its own, without an effect resetting state.
  const [failedUrl, setFailedUrl] = useState<string | null>(null);

  if (photoUrl && photoUrl !== failedUrl) {
    return (
      <img
        src={photoUrl}
        alt=""
        onError={() => setFailedUrl(photoUrl)}
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
