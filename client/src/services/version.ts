import { apiGet } from "@/lib/api";

export const fetchServerVersion = () =>
  apiGet<{ version: string }>("/api/version");
