import { Link } from "@tanstack/react-router";
import { CheckIcon, ChevronsUpDownIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { Logo } from "@/components/brand/logo";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { roleLabels } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { useSession } from "@/lib/session";
import { CreateOrganizationForm } from "./create-organization";

export function OrgSwitcher() {
  const org = useOrg();
  const session = useSession();
  const [creating, setCreating] = useState(false);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger className="flex h-10 items-center gap-2.5 rounded-md px-1.5 text-left transition-colors hover:bg-sidebar-accent data-[state=open]:bg-sidebar-accent">
          <Logo className="size-6" />
          <span className="min-w-0 flex-1">
            <span className="block truncate font-semibold text-[14px] leading-tight tracking-tight">
              {org.name}
            </span>
            <span className="block truncate text-[11.5px] text-muted-foreground leading-tight">İzci</span>
          </span>
          <ChevronsUpDownIcon className="size-3.5 shrink-0 text-muted-foreground" />
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
                {m.organization?.slug === org.slug ? <CheckIcon className="size-4 text-primary" /> : null}
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
