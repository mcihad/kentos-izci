import { cn } from "@kentos/components/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@kentos/components/ui/dropdown-menu";
import { Link } from "@tanstack/react-router";
import { CheckIcon, ChevronsUpDownIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { Logo } from "@/components/brand/logo";
import { roleLabels } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { useSession } from "@/lib/session";
import { CreateOrganizationForm } from "./create-organization";

/** The organization switch at the top of the rail (the design system's module switch). */
export function OrgSwitcher({ collapsed = false }: { collapsed?: boolean }) {
  const org = useOrg();
  const session = useSession();
  const [creating, setCreating] = useState(false);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          title={collapsed ? org.name : undefined}
          aria-label={collapsed ? `Organizasyon: ${org.name}` : undefined}
          className={cn(
            "flex h-11 w-full items-center gap-2.5 rounded-md border border-white/10 bg-white/[0.06] text-left text-white transition-colors duration-(--duration-fast) hover:bg-white/10 data-[state=open]:border-(--sidebar-accent-fg)/50 data-[state=open]:bg-white/10",
            collapsed ? "justify-center" : "px-2.5",
          )}
        >
          <span className="grid size-7 shrink-0 place-items-center rounded-sm bg-white/15">
            <Logo inverse className="size-5" />
          </span>
          {collapsed ? null : (
            <>
              <span className="grid min-w-0 flex-1 gap-0.5">
                <span className="font-medium text-(--sidebar-fg-muted) text-2xs uppercase leading-none tracking-(--tracking-caps)">
                  İzci
                </span>
                <span className="truncate font-semibold text-sm leading-tight">{org.name}</span>
              </span>
              <ChevronsUpDownIcon className="size-3.5 shrink-0 text-(--sidebar-fg-muted)" />
            </>
          )}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuLabel className="font-normal text-muted-foreground text-xs">
            Organizasyonlar
          </DropdownMenuLabel>
          {session?.memberships.map((m) => (
            <DropdownMenuItem key={m.organization?.id} asChild>
              <Link
                to="/o/$org"
                params={{ org: m.organization?.slug ?? "" }}
                className="flex items-center gap-2"
              >
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{m.organization?.name}</span>
                  <span className="block text-muted-foreground text-xs">{roleLabels[m.role]}</span>
                </span>
                {m.organization?.slug === org.slug ? <CheckIcon className="size-4 text-brand-ink" /> : null}
              </Link>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setCreating(true)}>
            <PlusIcon className="size-4" />
            Yeni organizasyon
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Yeni organizasyon</DialogTitle>
            <DialogDescription>
              Organizasyonlar projeleri, hataları ve üyeleri birbirinden tamamen ayırır.
            </DialogDescription>
          </DialogHeader>
          <CreateOrganizationForm onDone={() => setCreating(false)} />
        </DialogContent>
      </Dialog>
    </>
  );
}
