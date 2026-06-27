import { useState, type ReactNode } from "react";
import { Menu, PhoneCall } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Sidebar } from "./Sidebar";
import { ThemeToggle } from "./ThemeToggle";

export const AppShell = ({ children }: { children: ReactNode }) => {
  const [mobileOpen, setMobileOpen] = useState(false);

  return (
    <div className="flex min-h-screen flex-col">
      <header className="sticky top-0 z-30 flex items-center justify-between border-b bg-background/80 px-4 py-3 backdrop-blur sm:px-6">
        <div className="flex items-center gap-2">
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger asChild>
              <Button variant="outline" size="icon" className="md:hidden" aria-label="Accounts">
                <Menu className="h-4 w-4" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-72 p-0">
              <SheetTitle className="px-3 pt-3">Accounts</SheetTitle>
              <Sidebar onNavigate={() => setMobileOpen(false)} />
            </SheetContent>
          </Sheet>
          <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <PhoneCall className="h-4 w-4" />
          </span>
          <span className="text-lg font-semibold tracking-tight">StCalls</span>
        </div>
        <ThemeToggle />
      </header>
      <div className="flex flex-1">
        <aside className="hidden w-64 shrink-0 border-r md:block">
          <Sidebar />
        </aside>
        <main className="flex-1 px-4 py-6 sm:px-6">{children}</main>
      </div>
    </div>
  );
};
