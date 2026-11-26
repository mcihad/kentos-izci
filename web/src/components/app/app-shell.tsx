import {
  AppShellActions,
  AppShellBrand,
  AppShellHeader,
  AppShellMain,
  AppShellMenuButton,
  AppShellSearch,
  AppShell as Shell,
} from "@kentos/components/layout/app-shell";
import { Button } from "@kentos/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@kentos/components/ui/tooltip";
import { Link } from "@tanstack/react-router";
import { PaletteIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Logo } from "@/components/brand/logo";
import { useOrg } from "@/lib/org";
import { appearanceFlag, paletteFlag, shortcutHelpFlag, useFlag, useHotkeys } from "@/lib/shortcuts";
import { AppearanceSheet } from "./appearance-sheet";
import { CommandPalette } from "./command-palette";
import { NotificationsMenu } from "./notifications-menu";
import { ShortcutHelp } from "./shortcut-help";
import { AppSidebar } from "./sidebar";

/**
 * İzci's frame on the kit's AppShell: the sidebar (an icon rail when
 * collapsed on desktop, a drawer on phones), the app bar with the menu
 * button, brand (phones), search and actions, and the page.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const org = useOrg();
  const [paletteOpen, setPaletteOpen] = useFlag(paletteFlag);
  useHotkeys({
    "mod+k": () => setPaletteOpen(!paletteFlag.get()),
    "?": () => shortcutHelpFlag.set(true),
  });
  return (
    <Shell sidebarStorageKey="izci-sidebar">
      <AppSidebar />
      <AppShellHeader>
        <AppShellMenuButton />
        <AppShellBrand logo={<Logo className="size-6" />} title={org.name} asChild>
          <Link to="/o/$org" params={{ org: org.slug }} />
        </AppShellBrand>
        <AppShellSearch placeholder="Hata, proje ya da sayfa ara" onClick={() => setPaletteOpen(true)} />
        <AppShellActions>
          <NotificationsMenu />
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => appearanceFlag.set(true)}
                aria-label="Görünüm"
              >
                <PaletteIcon className="size-5 md:size-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">Görünüm</TooltipContent>
          </Tooltip>
        </AppShellActions>
      </AppShellHeader>
      <AppShellMain>{children}</AppShellMain>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ShortcutHelp />
      <AppearanceSheet />
    </Shell>
  );
}
