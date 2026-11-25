import { MenuIcon, PaletteIcon, PanelLeftCloseIcon, PanelLeftOpenIcon, SearchIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { Logo } from "@/components/brand/logo";
import { Kbd } from "@/components/common/kbd";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useOrg } from "@/lib/org";
import { appearanceFlag, paletteFlag, shortcutHelpFlag, useFlag, useHotkeys } from "@/lib/shortcuts";
import { cn } from "@/lib/utils";
import { AppearanceSheet } from "./appearance-sheet";
import { CommandPalette } from "./command-palette";
import { NotificationsMenu } from "./notifications-menu";
import { ShortcutHelp } from "./shortcut-help";
import { SidebarContent } from "./sidebar";

const collapsedKey = "izci-sidebar";

function readCollapsed() {
  try {
    return localStorage.getItem(collapsedKey) === "collapsed";
  } catch {
    return false;
  }
}

/**
 * The KentOS application frame: a dark rail on the left (collapsible on
 * desktop, a drawer on phones), a top bar with search and appearance, and the
 * page on the grey surface.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const org = useOrg();
  const [paletteOpen, setPaletteOpen] = useFlag(paletteFlag);
  const [mobileNav, setMobileNav] = useState(false);
  const [collapsed, setCollapsed] = useState(readCollapsed);
  useHotkeys({
    "mod+k": () => setPaletteOpen(!paletteFlag.get()),
    "?": () => shortcutHelpFlag.set(true),
  });
  const toggleRail = () => {
    const next = !collapsed;
    setCollapsed(next);
    try {
      if (next) localStorage.setItem(collapsedKey, "collapsed");
      else localStorage.removeItem(collapsedKey);
    } catch {
      // Not remembered; still applied.
    }
  };

  return (
    <div className="flex min-h-dvh bg-background">
      <aside
        className={cn(
          "on-dark sticky top-0 hidden h-dvh shrink-0 border-(--sidebar-border) border-r bg-(--sidebar-bg) text-(--sidebar-fg) transition-[width] duration-(--duration-panel) ease-(--ease-entrance) md:block",
          collapsed ? "w-(--sidebar-w-collapsed)" : "w-(--sidebar-w)",
        )}
      >
        <SidebarContent collapsed={collapsed} />
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-(--topbar-h) shrink-0 items-center gap-1.5 border-(--topbar-border) border-b bg-(--topbar-bg) px-2 md:gap-3 md:px-4">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            onClick={() => setMobileNav(true)}
            aria-label="Menüyü aç"
          >
            <MenuIcon className="size-5" />
          </Button>
          <Logo className="size-6 md:hidden" />
          <span className="truncate font-semibold text-lg text-strong md:hidden">{org.name}</span>
          <Button
            variant="ghost"
            size="icon"
            className="hidden md:inline-flex"
            onClick={toggleRail}
            aria-label={collapsed ? "Menüyü genişlet" : "Menüyü daralt"}
          >
            {collapsed ? <PanelLeftOpenIcon /> : <PanelLeftCloseIcon />}
          </Button>
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
      </div>
      <Sheet open={mobileNav} onOpenChange={setMobileNav}>
        <SheetContent
          side="left"
          className="on-dark w-[min(20rem,86vw)] gap-0 border-(--sidebar-border) bg-(--sidebar-bg) p-0 text-(--sidebar-fg) [&>button]:top-5 [&>button]:text-(--sidebar-fg)"
        >
          <SheetTitle className="sr-only">Menü</SheetTitle>
          <SheetDescription className="sr-only">Sayfalar arasında gezinin</SheetDescription>
          <SidebarContent inSheet onNavigate={() => setMobileNav(false)} />
        </SheetContent>
      </Sheet>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ShortcutHelp />
      <AppearanceSheet />
    </div>
  );
}
