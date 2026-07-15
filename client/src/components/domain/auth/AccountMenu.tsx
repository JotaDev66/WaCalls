import { useState } from "react";
import { KeyRound, LogOut, UserCog } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ChangePasswordDialog } from "@/components/domain/auth/ChangePasswordDialog";
import { useAuth } from "@/stores/auth";
import { logout } from "@/services/auth";
import { useT } from "@/hooks/useT";

export const AccountMenu = () => {
  const t = useT();
  const status = useAuth((s) => s.status);
  const [pwOpen, setPwOpen] = useState(false);

  if (status?.mode !== "login" || !status.authenticated) return null;

  const doLogout = async () => {
    await logout();
    window.location.reload();
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="icon" aria-label={t.password.menu}>
            <UserCog className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={() => setPwOpen(true)}>
            <KeyRound className="mr-2 h-4 w-4" />
            {t.password.menu}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={doLogout}>
            <LogOut className="mr-2 h-4 w-4" />
            {t.account.logout}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ChangePasswordDialog open={pwOpen} onOpenChange={setPwOpen} />
    </>
  );
};
