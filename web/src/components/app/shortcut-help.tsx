import { Kbd } from "@/components/common/kbd";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { shortcutGroups, shortcutHelpFlag, useFlag } from "@/lib/shortcuts";

export function ShortcutHelp() {
  const [open, setOpen] = useFlag(shortcutHelpFlag);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Klavye kısayolları</DialogTitle>
          <DialogDescription>Hataları fareye dokunmadan gözden geçirebilirsiniz.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-5 sm:grid-cols-2">
          {shortcutGroups.map((g) => (
            <section key={g.title} className={g.items.length > 4 ? "sm:row-span-2" : undefined}>
              <h3 className="mb-2 font-medium text-[13px]">{g.title}</h3>
              <dl className="grid gap-1.5">
                {g.items.map(([keys, label]) => (
                  <div key={label} className="flex items-center justify-between gap-3 text-[13px]">
                    <dt className="text-muted-foreground">{label}</dt>
                    <dd className="flex gap-1">
                      {keys.map((k) => (
                        <Kbd key={k}>{k}</Kbd>
                      ))}
                    </dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
