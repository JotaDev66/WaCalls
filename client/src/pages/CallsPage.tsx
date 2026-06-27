import { useEffect, useState } from "react";
import { PhoneCall, ShieldAlert, Key, Globe, Eye, EyeOff } from "lucide-react";
import { Dialer } from "@/components/domain/call/Dialer";
import { CallCard } from "@/components/domain/call/CallCard";
import { OtherCallsList } from "@/components/domain/call/OtherCallsList";
import { HistoryDrawer } from "@/components/domain/history/HistoryDrawer";
import { EmptyState } from "@/components/shared/EmptyState";
import { isMine, useCalls } from "@/stores/calls";
import { useSessions } from "@/stores/sessions";
import { toast } from "sonner";

export const CallsPage = ({ sid }: { sid: string }) => {
  const calls = useCalls((s) => s.calls);
  const session = useSessions((s) => s.sessions.find((x) => x.id === sid));
  const [tab, setTab] = useState<"calls" | "credentials">("calls");
  const [showSecret, setShowSecret] = useState(false);
  const [, force] = useState(0);

  useEffect(() => {
    const t = setInterval(() => force((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, []);

  const sessionCalls = calls.filter((c) => c.sessionId === sid && c.status !== "ended");
  const mine = sessionCalls.filter(isMine);
  const others = sessionCalls.filter((c) => !isMine(c));
  const sipUrl = (session?.sip_url && session.sip_url !== "127.0.0.1:5060") ? session.sip_url : `${window.location.hostname}:5060`;

  const copyToClipboard = (text?: string, label?: string) => {
    if (!text) return;
    navigator.clipboard.writeText(text);
    toast.success(`${label || "Copiado"} com sucesso!`);
  };

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      {/* Tabs */}
      <div className="flex border-b border-border">
        <button
          onClick={() => setTab("calls")}
          className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
            tab === "calls"
              ? "border-primary text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          }`}
        >
          Discador / Chamadas
        </button>
        <button
          onClick={() => setTab("credentials")}
          className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
            tab === "credentials"
              ? "border-primary text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          }`}
        >
          Integração / SIP
        </button>
      </div>

      {tab === "calls" ? (
        <div className="space-y-6">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-muted-foreground">
              {mine.length} active call{mine.length === 1 ? "" : "s"}
            </h2>
            <HistoryDrawer sid={sid} />
          </div>
          <Dialer sid={sid} />
          {mine.length > 0 ? (
            <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
              {mine.map((c) => (
                <CallCard key={c.callId} call={c} />
              ))}
            </div>
          ) : (
            <EmptyState
              icon={<PhoneCall className="h-6 w-6" />}
              title="Sem chamadas ativas"
              description="Disque um n?mero acima para iniciar."
            />
          )}
          <OtherCallsList calls={others} />
        </div>
      ) : (
        <div className="rounded-lg border border-border bg-card p-6 space-y-6">
          <div>
            <h3 className="text-lg font-semibold flex items-center gap-2 text-foreground">
              <Key className="h-5 w-5 text-primary" /> Credenciais de API
            </h3>
            <p className="text-xs text-muted-foreground mt-1">
              Use esta chave nos cabeçalhos das chamadas de API do sistema (`Authorization: Bearer &lt;API_Key&gt;`).
            </p>
            <div className="flex gap-2 shadow-sm rounded border border-input bg-muted/20 px-3 py-2 mt-3 items-center justify-between font-mono text-sm">
              <span className="truncate">{showSecret ? session?.api_key : "wac_????????????????????????????"}</span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setShowSecret(!showSecret)}
                  className="text-muted-foreground hover:text-foreground"
                >
                  {showSecret ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
                <button
                  type="button"
                  onClick={() => copyToClipboard(session?.api_key, "Chave API")}
                  className="text-primary hover:underline text-xs"
                >
                  Copiar
                </button>
              </div>
            </div>
          </div>

          <div className="border-t border-border pt-6">
            <h3 className="text-lg font-semibold flex items-center gap-2 text-foreground">
              <Globe className="h-5 w-5 text-emerald-500" /> Servidor SIP
            </h3>
            <p className="text-xs text-muted-foreground mt-1">
              Configure estas credenciais em seu Softphone ou Gateway SIP local para encaminhar chamadas via Asterisk.
            </p>
            
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mt-4 text-sm">
              <div className="rounded border border-border p-3 space-y-1">
                <p className="text-xs text-muted-foreground">Usuário SIP</p>
                <div className="flex items-center justify-between">
                  <span className="font-mono text-foreground font-semibold">{session?.sip_user || "sip_none"}</span>
                  <button onClick={() => copyToClipboard(session?.sip_user, "SIP User")} className="text-primary text-xs hover:underline">Copiar</button>
                </div>
              </div>
              <div className="rounded border border-border p-3 space-y-1">
                <p className="text-xs text-muted-foreground">Senha SIP</p>
                <div className="flex items-center justify-between">
                  <span className="font-mono text-foreground font-semibold">{showSecret ? session?.sip_pass : "????????"}</span>
                  <button onClick={() => copyToClipboard(session?.sip_pass, "SIP Pass")} className="text-primary text-xs hover:underline">Copiar</button>
                </div>
              </div>
              <div className="rounded border border-border p-3 md:col-span-2 space-y-1">
                <p className="text-xs text-muted-foreground">Endereço Server SIP / URL</p>
                <div className="flex items-center justify-between">
                  <span className="font-mono text-foreground">{sipUrl}</span>
                  <button onClick={() => copyToClipboard(sipUrl, "SIP Server")} className="text-primary text-xs hover:underline">Copiar</button>
                </div>
              </div>
            </div>
          </div>
          
          <div className="flex items-center gap-2 bg-amber-500/10 border border-amber-500/20 text-amber-500 rounded p-3 text-xs leading-relaxed">
            <ShieldAlert className="h-4 w-4 shrink-0" />
            <span>Mantenha sua chave de API e senha do SIP confidenciais. Compartilhar estes dados permite acesso completo ao envio de mensagens e chamadas.</span>
          </div>
        </div>
      )}
    </div>
  );
};
