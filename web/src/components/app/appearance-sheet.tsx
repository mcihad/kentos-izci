import { AppearancePanel } from "@kentos/components/common/appearance-panel";
import { useAppearance } from "@kentos/components/lib/theme";
import { Button } from "@kentos/components/ui/button";
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetTitle } from "@kentos/components/ui/sheet";
import { PaletteIcon, RotateCcwIcon, XIcon } from "lucide-react";
import { appearanceFlag, useFlag } from "@/lib/shortcuts";

/**
 * The theme editor in a side sheet: the kit's AppearancePanel, opened from
 * the top bar or with its shortcut. Choices are kept in this browser.
 */
export function AppearanceSheet() {
  const [open, setOpen] = useFlag(appearanceFlag);
  const { resetAppearance } = useAppearance();
  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-[min(22rem,100vw)] gap-0 p-0 sm:max-w-none"
      >
        <header className="flex h-(--topbar-h) shrink-0 items-center gap-2 border-b pr-2 pl-5">
          <PaletteIcon className="size-4 text-brand-ink" />
          <SheetTitle className="flex-1 font-semibold text-md">Görünüm</SheetTitle>
          <Button variant="ghost" size="sm" onClick={resetAppearance}>
            <RotateCcwIcon />
            Sıfırla
          </Button>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Kapat">
              <XIcon />
            </Button>
          </SheetClose>
        </header>
        <SheetDescription className="sr-only">
          Tema, yazı tipi, yoğunluk, köşeler, ana renk ve kart görünümü
        </SheetDescription>
        <AppearancePanel className="flex-1 overflow-y-auto p-5" />
        <footer className="shrink-0 border-t bg-sunken px-5 py-3 text-muted-foreground text-sm">
          Seçimler bu tarayıcıda saklanır.
        </footer>
      </SheetContent>
    </Sheet>
  );
}
