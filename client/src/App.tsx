import { useEffect } from "react";
import { PlusCircle } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { AppShell } from "@/components/layout/AppShell";
import { CallsPage } from "@/pages/CallsPage";
import { ContactsPage } from "@/components/domain/contacts/ContactsPage";
import { SessionPairing } from "@/components/domain/session/SessionPairing";
import { SessionHeader } from "@/components/domain/session/SessionHeader";
import { SessionNameDialog } from "@/components/domain/session/SessionNameDialog";
import { IncomingCallModal } from "@/components/domain/call/IncomingCallModal";
import { EmptyState } from "@/components/shared/EmptyState";
import { ensureSessionsWired, useSessions } from "@/stores/sessions";
import { ensureCallsWired } from "@/stores/calls";
import { ensureConnectionWired } from "@/stores/connection";
import { useTheme } from "@/stores/theme";
import { setOnUnauthorized } from "@/lib/api";
import { setAuthStatus } from "@/stores/auth";
import { AuthGate } from "@/components/domain/auth/AuthGate";
import { Omnibox } from "@/components/domain/omnibox/Omnibox";
import { OnboardingChecklist } from "@/components/domain/onboarding/OnboardingChecklist";
import { useOnboarding } from "@/stores/onboarding";
import { useNav } from "@/stores/nav";
import { useT } from "@/hooks/useT";

export const App = () => {
  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  const theme = useTheme((s) => s.theme);
  const onboardingDismissed = useOnboarding((s) => s.dismissed);
  const view = useNav((s) => s.view);
  const setView = useNav((s) => s.setView);
  const t = useT();

  useEffect(() => {
    setOnUnauthorized(() => setAuthStatus({ authenticated: false }));
    ensureConnectionWired();
    ensureSessionsWired();
    ensureCallsWired();
  }, []);

  useEffect(() => {
    setView("console");
  }, [activeId, setView]);

  const active = sessions.find((s) => s.id === activeId) ?? null;

  return (
    <TooltipProvider delayDuration={200}>
      <AppShell>
        <div className="space-y-6">
          <OnboardingChecklist />
          {sessions.length === 0 ? (
            onboardingDismissed ? (
              <EmptyState
                icon={<PlusCircle className="h-6 w-6" />}
                title={t.app.noAccountsTitle}
                description={t.app.noAccountsDescription}
              />
            ) : null
          ) : active ? (
            <>
              <SessionHeader session={active} />
              {active.paired ? (
                view === "contacts" ? (
                  <ContactsPage sid={active.id} />
                ) : (
                  <CallsPage sid={active.id} />
                )
              ) : (
                <SessionPairing session={active} />
              )}
            </>
          ) : (
            <EmptyState
              title={t.app.selectAccountTitle}
              description={t.app.selectAccountDescription}
            />
          )}
        </div>
      </AppShell>
      <IncomingCallModal />
      <Omnibox />
      <SessionNameDialog />
      <AuthGate />
      <Toaster theme={theme} position="top-right" richColors closeButton />
    </TooltipProvider>
  );
};
