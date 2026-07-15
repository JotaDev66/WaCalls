import { useState } from "react";
import { Users } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  CommandDialog,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { ContactAvatar } from "@/components/domain/contacts/ContactAvatar";
import { useContacts } from "@/hooks/useContacts";
import { filterContacts } from "@/lib/contacts";
import { useT } from "@/hooks/useT";

export const ContactPicker = ({
  sid,
  onPick,
}: {
  sid: string;
  onPick: (phone: string) => void;
}) => {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { data } = useContacts(sid, open);
  const list = filterContacts(query, data ?? []);

  const close = () => {
    setOpen(false);
    setQuery("");
  };

  return (
    <>
      <Button
        type="button"
        variant="outline"
        className="w-full"
        onClick={() => setOpen(true)}
      >
        <Users className="h-4 w-4" />
        {t.contacts.pick}
      </Button>
      <CommandDialog
        open={open}
        onOpenChange={(o) => (o ? setOpen(true) : close())}
        shouldFilter={false}
        title={t.contacts.pickTitle}
      >
        <CommandInput
          value={query}
          onValueChange={setQuery}
          placeholder={t.contacts.searchPlaceholder}
        />
        <CommandList>
          <CommandEmpty>{t.contacts.noResults}</CommandEmpty>
          {list.map((c) => (
            <CommandItem
              key={c.jid}
              value={`${c.name} ${c.phone}`}
              onSelect={() => {
                onPick(c.phone);
                close();
              }}
            >
              <ContactAvatar name={c.name} />
              <span className="flex-1 truncate">{c.name}</span>
              <span className="font-mono text-xs text-muted-foreground">
                {c.phone}
              </span>
            </CommandItem>
          ))}
        </CommandList>
      </CommandDialog>
    </>
  );
};
