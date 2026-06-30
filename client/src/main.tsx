import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import { queryClient } from "@/lib/query";
import "@/styles/index.css";
import AuthGate from "@/components/AuthGate";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AuthGate>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </AuthGate>
  </StrictMode>,
);
