import { useState } from "react";
import { Delete, Phone } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { DeviceSelector } from "@/components/form/DeviceSelector";
import { DialPad } from "@/components/domain/call/DialPad";
import { ContactPicker } from "@/components/domain/contacts/ContactPicker";
import { useStartCall } from "@/hooks/useStartCall";
import { useDevices } from "@/stores/devices";
import { useT } from "@/hooks/useT";

export const Dialer = ({ sid }: { sid: string }) => {
  const [phone, setPhone] = useState("");
  const micId = useDevices((s) => s.micId);
  const startCall = useStartCall(sid, micId);
  const t = useT();

  const submit = () => {
    if (!phone.trim() || startCall.isPending) return;
    startCall.mutate(
      { phone: phone.trim() },
      { onSuccess: () => setPhone("") },
    );
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.dialer.title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <DeviceSelector />
        <div className="flex items-center gap-2">
          <Input
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
            placeholder={t.dialer.phonePlaceholder}
            inputMode="tel"
            className="h-11 flex-1 text-center font-mono text-lg tracking-wide"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => setPhone((p) => p.slice(0, -1))}
            disabled={!phone}
            aria-label={t.dialer.backspace}
          >
            <Delete className="h-4 w-4" />
          </Button>
        </div>
        <ContactPicker sid={sid} onPick={(p) => setPhone(p)} />
        <DialPad onKey={(c) => setPhone((p) => p + c)} />
        <Button
          className="w-full"
          onClick={submit}
          disabled={startCall.isPending || !phone.trim()}
        >
          <Phone className="h-4 w-4" />
          {startCall.isPending ? t.dialer.calling : t.dialer.call}
        </Button>
      </CardContent>
    </Card>
  );
};
