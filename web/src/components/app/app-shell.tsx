import { MenuIcon, SearchIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { Logo } from "@/components/brand/logo";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { useOrg } from "@/lib/org";
import { paletteFlag, shortcutHelpFlag, useFlag, useHotkeys } from "@/lib/shortcuts";
import { CommandPalette } from "./command-palette";
import { ShortcutHelp } from "./shortcut-help";
import { SidebarContent } from "./sidebar";

export function AppShell({ children }: { children: ReactNode }) {
  const org = useOrg();
  const [paletteOpen, setPaletteOpen] = useFlag(paletteFlag);
  const [mobileNav, setMobileNav] = useState(false);
  useHotkeys({
    "mod+k": () => setPaletteOpen(!paletteFlag.get()),
    "?": () => shortcutHelpFlag.set(true),
  });

  return (
    <div className="flex min-h-dvh bg-background">
      <aside className="sticky top-0 hidden h-dvh w-64 shrink-0 border-sidebar-border border-r bg-sidebar md:block">
        <SidebarContent onSearch={() => setPaletteOpen(true)} />
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-1.5 border-b bg-background/90 px-2 backdrop-blur-md md:hidden">
          <Button variant="ghost" size="icon" onClick={() => setMobileNav(true)} aria-label="Menüyü aç">
            <MenuIcon className="size-5" />
          </Button>
          <Logo className="size-6" />
          <span className="truncate font-semibold text-lg">{org.name}</span>
          <Button
            variant="ghost"
            size="icon"
            className="ml-auto"
            onClick={() => setPaletteOpen(true)}
            aria-label="Ara"
          >
            <SearchIcon className="size-5" />
          </Button>
        </header>
        <main className="min-w-0 flex-1">{children}</main>
      </div>
      <Sheet open={mobileNav} onOpenChange={setMobileNav}>
        <SheetContent side="left" className="w-[min(20rem,86vw)] gap-0 border-sidebar-border bg-sidebar p-0">
          <SheetTitle className="sr-only">Menü</SheetTitle>
          <SheetDescription className="sr-only">Sayfalar arasında gezinin</SheetDescription>
          <SidebarContent
            onNavigate={() => setMobileNav(false)}
            onSearch={() => {
              setMobileNav(false);
              setPaletteOpen(true);
            }}
          />
        </SheetContent>
      </Sheet>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ShortcutHelp />
    </div>
  );
}
