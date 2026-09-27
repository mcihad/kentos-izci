import { MonitorIcon, MoonIcon, PaletteIcon, RotateCcwIcon, SunIcon, XIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Segmented } from "@/components/common/segmented";
import { Button } from "@/components/ui/button";
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { appearanceFlag, useFlag } from "@/lib/shortcuts";
import { type Accent, type FontChoice, useAppearance } from "@/lib/theme";
import { cn } from "@/lib/utils";

const fonts: { value: FontChoice; label: string; hint: string; family: string }[] = [
  { value: "jakarta", label: "Plus Jakarta Sans", hint: "Varsayılan", family: '"Plus Jakarta Sans"' },
  { value: "inter", label: "Inter", hint: "Yoğun tablolar için", family: '"Inter"' },
  { value: "plex", label: "IBM Plex Sans", hint: "Resmî, belge gibi", family: '"IBM Plex Sans"' },
];

const accents: { value: Accent; label: string; color: string }[] = [
  { value: "navy", label: "Lacivert", color: "var(--navy-800)" },
  { value: "teal", label: "Petrol", color: "var(--teal-600)" },
  { value: "violet", label: "Mor", color: "var(--violet-700)" },
  { value: "green", label: "Yeşil", color: "var(--green-700)" },
  { value: "amber", label: "Kehribar", color: "var(--amber-600)" },
  { value: "neutral", label: "Nötr", color: "var(--neutral-700)" },
];

// A choice drawn as a card: a label around a visually hidden native radio.
const choiceClass =
  "cursor-pointer rounded-md border transition-colors duration-(--duration-fast) has-[:focus-visible]:shadow-(--focus-shadow)";

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-2.5">
      <h3 className="font-semibold text-2xs text-muted-foreground uppercase tracking-(--tracking-caps)">
        {title}
      </h3>
      {children}
    </section>
  );
}

/**
 * The theme editor: every control switches one scope class of the KentOS
 * design system on <html>. Choices are kept in this browser.
 */
export function AppearanceSheet() {
  const [open, setOpen] = useFlag(appearanceFlag);
  const { appearance: a, setAppearance, resetAppearance } = useAppearance();
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
        <div className="grid flex-1 content-start gap-6 overflow-y-auto p-5">
          <Group title="Tema">
            <Segmented
              label="Tema"
              value={a.mode}
              onChange={(mode) => setAppearance({ mode })}
              options={[
                { value: "light", label: "Açık", icon: SunIcon },
                { value: "dark", label: "Koyu", icon: MoonIcon },
                { value: "system", label: "Sistem", icon: MonitorIcon },
              ]}
            />
          </Group>
          <Group title="Yazı tipi">
            <fieldset className="grid min-w-0 gap-1.5">
              <legend className="sr-only">Yazı tipi</legend>
              {fonts.map((f) => {
                const on = a.font === f.value;
                return (
                  <label
                    key={f.value}
                    className={cn(
                      choiceClass,
                      "flex items-center gap-3 px-3 py-2.5",
                      on ? "border-(--border-brand) bg-selected" : "hover:bg-(--surface-hover)",
                    )}
                  >
                    <input
                      type="radio"
                      name="appearance-font"
                      value={f.value}
                      checked={on}
                      onChange={() => setAppearance({ font: f.value })}
                      className="sr-only"
                    />
                    <span className="grid min-w-0 flex-1 gap-0.5">
                      <span
                        className="truncate font-medium text-md"
                        style={{ fontFamily: `${f.family}, sans-serif` }}
                      >
                        {f.label}
                      </span>
                      <span className="text-muted-foreground text-sm">{f.hint}</span>
                    </span>
                    <span
                      aria-hidden="true"
                      className="font-semibold text-strong text-xl tracking-[-0.02em]"
                      style={{ fontFamily: `${f.family}, sans-serif` }}
                    >
                      Ağ
                    </span>
                  </label>
                );
              })}
            </fieldset>
          </Group>
          <Group title="Yoğunluk">
            <Segmented
              label="Yoğunluk"
              value={a.density}
              onChange={(density) => setAppearance({ density })}
              options={[
                { value: "compact", label: "Sıkı" },
                { value: "standard", label: "Standart" },
                { value: "roomy", label: "Geniş" },
              ]}
            />
          </Group>
          <Group title="Köşeler">
            <Segmented
              label="Köşeler"
              value={a.radius}
              onChange={(radius) => setAppearance({ radius })}
              options={[
                { value: "square", label: "Köşeli" },
                { value: "tight", label: "Hafif" },
                { value: "moderate", label: "Orta" },
                { value: "soft", label: "Yuvarlak" },
              ]}
            />
          </Group>
          <Group title="Ana renk">
            <fieldset className="grid min-w-0 grid-cols-3 gap-1.5">
              <legend className="sr-only">Ana renk</legend>
              {accents.map((c) => {
                const on = a.accent === c.value;
                return (
                  <label
                    key={c.value}
                    className={cn(
                      choiceClass,
                      "grid justify-items-center gap-1.5 px-1.5 py-2.5",
                      on ? "border-(--border-brand) bg-selected" : "hover:bg-(--surface-hover)",
                    )}
                  >
                    <input
                      type="radio"
                      name="appearance-accent"
                      value={c.value}
                      checked={on}
                      onChange={() => setAppearance({ accent: c.value })}
                      className="sr-only"
                    />
                    <span
                      aria-hidden="true"
                      className="size-[22px] rounded-full"
                      style={{
                        background: c.color,
                        boxShadow: on ? `0 0 0 2px var(--surface-card), 0 0 0 4px ${c.color}` : undefined,
                      }}
                    />
                    <span className="font-medium text-xs">{c.label}</span>
                  </label>
                );
              })}
            </fieldset>
            <p className="text-muted-foreground text-sm">
              Düğmeler, sekmeler, bağlantılar ve kenar çubuğu bu renge geçer.
            </p>
          </Group>
          <Group title="Kartlar">
            <Segmented
              label="Kartlar"
              value={a.elevation}
              onChange={(elevation) => setAppearance({ elevation })}
              options={[
                { value: "flat", label: "Düz" },
                { value: "bordered", label: "Kenarlıklı" },
                { value: "raised", label: "Gölgeli" },
              ]}
            />
          </Group>
        </div>
        <footer className="shrink-0 border-t bg-sunken px-5 py-3 text-muted-foreground text-sm">
          Seçimler bu tarayıcıda saklanır.
        </footer>
      </SheetContent>
    </Sheet>
  );
}
