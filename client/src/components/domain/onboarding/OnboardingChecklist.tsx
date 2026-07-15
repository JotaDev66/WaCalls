import { useEffect, useState } from "react";
import { Check, X } from "lucide-react";
import { toast } from "sonner";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  onboardingSteps,
  onboardingComplete,
  currentStep,
  type OnboardingStepKey,
} from "@/lib/onboarding";
import { setActiveSession, useSessions } from "@/stores/sessions";
import { useCalls } from "@/stores/calls";
import { useOnboarding } from "@/stores/onboarding";
import { createSession } from "@/services/sessions";
import { useT } from "@/hooks/useT";

export const OnboardingChecklist = () => {
  const sessions = useSessions((s) => s.sessions);
  const anyConnected = useCalls((s) =>
    s.calls.some((c) => c.status === "connected"),
  );
  const firstCallDone = useOnboarding((s) => s.firstCallDone);
  const dismissed = useOnboarding((s) => s.dismissed);
  const markFirstCall = useOnboarding((s) => s.markFirstCall);
  const dismiss = useOnboarding((s) => s.dismiss);
  const t = useT();
  const [creating, setCreating] = useState(false);

  const hasSessions = sessions.length > 0;
  const steps = onboardingSteps(
    sessions.some((s) => s.paired),
    firstCallDone,
  );
  const complete = onboardingComplete(steps);
  const active = currentStep(steps);

  useEffect(() => {
    if (anyConnected) markFirstCall();
  }, [anyConnected, markFirstCall]);

  useEffect(() => {
    if (complete) dismiss();
  }, [complete, dismiss]);

  if (dismissed || complete) return null;

  const onCreate = async () => {
    setCreating(true);
    try {
      const { id } = await createSession("WhatsApp");
      setActiveSession(id);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setCreating(false);
    }
  };

  const label: Record<OnboardingStepKey, string> = {
    link: t.onboarding.link,
    call: t.onboarding.call,
  };

  return (
    <Card className="border-primary/30 bg-accent/40">
      <CardContent className="space-y-3 p-4">
        <div className="flex items-start justify-between">
          <div>
            <p className="font-semibold">{t.onboarding.title}</p>
            <p className="text-sm text-muted-foreground">
              {t.onboarding.subtitle}
            </p>
          </div>
          <button
            type="button"
            onClick={dismiss}
            aria-label={t.onboarding.dismiss}
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <ol className="space-y-2">
          {steps.map((step, i) => (
            <li key={step.key} className="flex items-start gap-3">
              <span
                className={cn(
                  "mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold",
                  step.done
                    ? "bg-primary text-primary-foreground"
                    : active === step.key
                      ? "bg-primary/15 text-primary ring-1 ring-primary/40"
                      : "bg-muted text-muted-foreground",
                )}
              >
                {step.done ? <Check className="h-3 w-3" /> : i + 1}
              </span>
              <div className="min-w-0 flex-1">
                <p
                  className={cn(
                    "text-sm",
                    step.done && "text-muted-foreground line-through",
                  )}
                >
                  {label[step.key]}
                </p>
                {active === "link" && step.key === "link" && !hasSessions && (
                  <Button
                    size="sm"
                    className="mt-2"
                    disabled={creating}
                    onClick={onCreate}
                  >
                    {t.onboarding.createCta}
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ol>
      </CardContent>
    </Card>
  );
};
