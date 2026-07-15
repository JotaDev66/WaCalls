import { useMemo, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useSaveContact } from "@/hooks/useContacts";
import { contactErrorKey, findContactByPhone } from "@/lib/contacts";
import { useT } from "@/hooks/useT";
import type { Contact } from "@/types/contact";

type Props = {
  sid: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  editing?: Contact;
  existing: Contact[];
};

export const ContactForm = ({
  sid,
  open,
  onOpenChange,
  editing,
  existing,
}: Props) => {
  const t = useT();
  const save = useSaveContact(sid);
  const [phone, setPhone] = useState(editing?.phone ?? "");
  const [name, setName] = useState(editing?.name ?? "");
  const [errorKey, setErrorKey] = useState<
    "notOnWhatsApp" | "appStateSyncing" | "saveError" | null
  >(null);

  const duplicate = useMemo(
    () => (editing ? undefined : findContactByPhone(existing, phone)),
    [editing, existing, phone],
  );

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!phone.trim() || !name.trim()) return;
    setErrorKey(null);
    save.mutate(
      { phone, name },
      {
        onSuccess: () => onOpenChange(false),
        onError: (err: Error) => setErrorKey(contactErrorKey(err.message)),
      },
    );
  };

  const switchToEdit = () => {
    if (duplicate) setName(duplicate.name);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editing ? t.contacts.editContact : t.contacts.newContact}
          </DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="contact-phone">{t.contacts.phoneLabel}</Label>
            <Input
              id="contact-phone"
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              placeholder={t.dialer.phonePlaceholder}
              disabled={!!editing || save.isPending}
              inputMode="tel"
            />
            {duplicate && (
              <p className="text-xs text-amber-600 dark:text-amber-500">
                {t.contacts.duplicateNotice(duplicate.name)}{" "}
                <button
                  type="button"
                  onClick={switchToEdit}
                  className="underline underline-offset-2"
                >
                  {t.contacts.editThisInstead}
                </button>
              </p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="contact-name">{t.contacts.nameLabel}</Label>
            <Input
              id="contact-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={save.isPending}
            />
          </div>
          {errorKey && (
            <p className="text-sm text-destructive">{t.contacts[errorKey]}</p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={save.isPending}
            >
              {t.common.cancel}
            </Button>
            <Button
              type="submit"
              disabled={save.isPending || !phone.trim() || !name.trim()}
            >
              {save.isPending ? t.contacts.saving : t.contacts.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
};
