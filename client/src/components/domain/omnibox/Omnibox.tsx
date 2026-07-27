import { useEffect, useState } from "react";
import { ArrowRight, Phone, Plus } from "lucide-react";
import {
  CommandDialog,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { setActiveSession, useSessions } from "@/stores/sessions";
import { useDevices } from "@/stores/devices";
import { useStartCall } from "@/hooks/useStartCall";
import { openCreateSession } from "@/stores/session-name-dialog";
import { looksLikePhone, omniboxItems, type OmniItem } from "@/lib/omnibox";
import { useT } from "@/hooks/useT";

export const Omnibox = () => {
  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  const micId = useDevices((s) => s.micId);
  const t = useT();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const active = sessions.find((s) => s.id === activeId);
  const dialSid = active?.paired
    ? active.id
    : sessions.find((s) => s.paired)?.id;
  const startCall = useStartCall(dialSid ?? "", micId);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const close = () => {
    setOpen(false);
    setQuery("");
  };

  const run = (item: OmniItem) => {
    if (item.kind === "dial" && dialSid) {
      setActiveSession(dialSid);
      startCall.mutate({ phone: item.phone });
    } else if (item.kind === "session") {
      setActiveSession(item.id);
    } else if (item.kind === "new-session") {
      const prefill =
        query.trim() && !looksLikePhone(query) ? query.trim() : "";
      openCreateSession(prefill);
    }
    close();
  };

  const items = omniboxItems(query, sessions, !!dialSid);

  return (
    <CommandDialog
      open={open}
      onOpenChange={(o) => (o ? setOpen(true) : close())}
      shouldFilter={false}
      title={t.omnibox.placeholder}
    >
      <CommandInput
        value={query}
        onValueChange={setQuery}
        placeholder={t.omnibox.placeholder}
      />
      <CommandList>
        <CommandEmpty>{t.omnibox.empty}</CommandEmpty>
        {items.map((item) => {
          if (item.kind === "dial") {
            return (
              <CommandItem
                key="dial"
                value={`dial ${item.phone}`}
                onSelect={() => run(item)}
              >
                <Phone />
                {t.omnibox.dial(item.phone)}
              </CommandItem>
            );
          }
          if (item.kind === "session") {
            return (
              <CommandItem
                key={item.id}
                value={`session ${item.name}`}
                onSelect={() => run(item)}
              >
                <ArrowRight />
                {t.omnibox.switchTo(item.name)}
              </CommandItem>
            );
          }
          return (
            <CommandItem
              key="new-session"
              value="new session"
              onSelect={() => run(item)}
            >
              <Plus />
              {t.sessions.newSession}
            </CommandItem>
          );
        })}
      </CommandList>
    </CommandDialog>
  );
};
