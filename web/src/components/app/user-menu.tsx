import { useMutation } from "@connectrpc/connect-query";
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
import { UserAvatar } from "@/components/common/user-avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { baseTransport } from "@/lib/api";
import { useOrg } from "@/lib/org";
import { useSession } from "@/lib/session";
import { appearanceFlag, useShortcutHelp } from "@/lib/shortcuts";
import { type ThemeMode, useAppearance } from "@/lib/theme";
import { cn } from "@/lib/utils";

export function UserMenu({ collapsed = false }: { collapsed?: boolean }) {
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
      <DropdownMenuTrigger
        title={collapsed ? user?.name || user?.email : undefined}
        aria-label={collapsed ? "Hesap menüsü" : undefined}
        className={cn(
          "flex w-full items-center gap-2.5 rounded-md p-1.5 text-left transition-colors duration-(--duration-fast) hover:bg-(--sidebar-item-hover) data-[state=open]:bg-(--sidebar-item-hover)",
          collapsed && "justify-center",
        )}
      >
        <UserAvatar
          name={user?.name || user?.email || ""}
          src={user?.avatarUrl}
          className="size-7"
          fallbackClassName="bg-white/15 text-white"
        />
        {collapsed ? null : (
          <span className="min-w-0 flex-1">
            <span className="block truncate font-semibold text-sm text-white leading-tight">
              {user?.name || user?.email}
            </span>
            <span className="block truncate text-(--sidebar-fg-muted) text-2xs leading-tight">
              {user?.email}
            </span>
          </span>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side={collapsed ? "right" : "top"} className="w-60">
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
