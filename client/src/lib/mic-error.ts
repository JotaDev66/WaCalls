import type { Messages } from "@/i18n/messages";

export type MicErrorKind = "denied" | "busy" | "notfound";

export const micErrorKind = (err: unknown): MicErrorKind | null => {
  const name =
    typeof err === "object" && err !== null && "name" in err
      ? String((err as { name: unknown }).name)
      : "";
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
      return "denied";
    case "NotReadableError":
    case "AbortError":
      return "busy";
    case "NotFoundError":
    case "OverconstrainedError":
      return "notfound";
    default:
      return null;
  }
};

export const micErrorMessage = (
  err: unknown,
  m: Pick<Messages["calls"], "micBusy" | "micDenied" | "micNotFound">,
): string | null => {
  switch (micErrorKind(err)) {
    case "denied":
      return m.micDenied;
    case "busy":
      return m.micBusy;
    case "notfound":
      return m.micNotFound;
    default:
      return null;
  }
};
