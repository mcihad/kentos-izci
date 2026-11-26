import { Kbd } from "@kentos/components/common/kbd";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@kentos/components/layout/sidebar";
import { Button } from "@kentos/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@kentos/components/ui/tooltip";
import { PaletteIcon, SearchIcon } from "lucide-react";
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
 * The KentOS application frame: the kit's sidebar on the left (an icon rail
 * when collapsed on desktop, a drawer on phones), a top bar with search and
 * appearance, and the page on the grey surface.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const org = useOrg();
  const [paletteOpen, setPaletteOpen] = useFlag(paletteFlag);
  useHotkeys({
    "mod+k": () => setPaletteOpen(!paletteFlag.get()),
    "?": () => shortcutHelpFlag.set(true),
  });
  return (
    <SidebarProvider storageKey="izci-sidebar" className="bg-background">
      <AppSidebar />
      <SidebarInset>
        <header className="sticky top-0 z-30 flex h-(--topbar-h) shrink-0 items-center gap-1.5 border-(--topbar-border) border-b bg-(--topbar-bg) px-2 md:gap-3 md:px-4 print:hidden">
          <SidebarTrigger />
          <Logo className="size-6 md:hidden" />
          <span className="truncate font-semibold text-lg text-strong md:hidden">{org.name}</span>
          <button
            type="button"
            onClick={() => setPaletteOpen(true)}
            className="hidden h-(--control-h-sm) w-[min(21rem,38vw)] items-center gap-2 rounded-md border border-(--input-border) bg-(--input-bg) px-2.5 text-muted-foreground text-sm transition-colors duration-(--duration-fast) hover:border-(--input-border-hover) hover:text-foreground focus-visible:shadow-(--focus-shadow) focus-visible:outline-none md:flex"
          >
            <SearchIcon className="size-4 shrink-0" />
            <span className="flex-1 truncate text-left">Hata, proje ya da sayfa ara</span>
            <Kbd>⌘K</Kbd>
          </button>
          <div className="ml-auto flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden"
              onClick={() => setPaletteOpen(true)}
              aria-label="Ara"
            >
              <SearchIcon className="size-5" />
            </Button>
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
          </div>
        </header>
        <main className="min-w-0 flex-1">
          <div className="mx-auto w-full max-w-(--content-max-wide)">{children}</div>
        </main>
      </SidebarInset>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ShortcutHelp />
      <AppearanceSheet />
    </SidebarProvider>
  );
}
