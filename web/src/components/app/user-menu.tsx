import { useMutation } from "@connectrpc/connect-query";
import { SidebarUser, useSidebar } from "@kentos/components/layout/sidebar";
import { type ThemeMode, useAppearance } from "@kentos/components/lib/theme";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@kentos/components/ui/dropdown-menu";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  KeyboardIcon,
  LogOutIcon,
  MonitorIcon,
  MoonIcon,
  PaletteIcon,
  SunIcon,
  UserIcon,
} from "lucide-react";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { baseTransport } from "@/lib/api";
import { useOrg } from "@/lib/org";
import { useSession } from "@/lib/session";
import { appearanceFlag, useShortcutHelp } from "@/lib/shortcuts";

export function UserMenu() {
  const { iconOnly } = useSidebar();
  const session = useSession();
  const org = useOrg();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { appearance, setAppearance } = useAppearance();
  const showShortcuts = useShortcutHelp();
  const logout = useMutation(AuthService.method.logout, {
    transport: baseTransport,
    onSettled: () => {
      qc.clear();
      void navigate({ to: "/login" });
    },
  });
  const user = session?.user;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <SidebarUser name={user?.name || user?.email || ""} description={user?.email} src={user?.avatarUrl} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side={iconOnly ? "right" : "top"} className="w-60">
        <DropdownMenuLabel className="font-normal">
          <span className="block truncate font-medium">{user?.name}</span>
          <span className="block truncate text-muted-foreground text-xs">{user?.email}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/o/$org/account" params={{ org: org.slug }}>
            <UserIcon className="size-4" />
            Hesabım
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={showShortcuts}>
          <KeyboardIcon className="size-4" />
          Klavye kısayolları
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => appearanceFlag.set(true)}>
          <PaletteIcon className="size-4" />
          Görünümü özelleştir
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="font-normal text-muted-foreground text-xs">Tema</DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={appearance.mode}
          onValueChange={(v) => setAppearance({ mode: v as ThemeMode })}
        >
          <DropdownMenuRadioItem value="dark">
            <MoonIcon className="size-4" />
            Koyu
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="light">
            <SunIcon className="size-4" />
            Açık
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">
            <MonitorIcon className="size-4" />
            Sistem
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => logout.mutate({})}>
          <LogOutIcon className="size-4" />
          Çıkış yap
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
