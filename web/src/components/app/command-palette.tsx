import { createClient } from "@connectrpc/connect";
import { useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  BoxIcon,
  CornerDownLeftIcon,
  KeyRoundIcon,
  LogOutIcon,
  MonitorIcon,
  MoonIcon,
  PaletteIcon,
  SearchIcon,
  SettingsIcon,
  SunIcon,
  UserIcon,
} from "lucide-react";
import { useState } from "react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { baseTransport } from "@/lib/api";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { appearanceFlag } from "@/lib/shortcuts";
import { setTheme } from "@/lib/theme";
import { primaryNav } from "./nav";

const shortIdPattern = /^[a-z][a-z0-9-]*-[0-9a-z]+$/i;

export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const org = useOrg();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [value, setValue] = useState("");
  const projects = useQuery(ProjectService.method.listProjects, {}, { enabled: open, staleTime: 60_000 });
  const text = value.trim();

  const run = (fn: () => void) => {
    onOpenChange(false);
    setValue("");
    fn();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="top-[18%] translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-xl"
      >
        <DialogTitle className="sr-only">Arama ve komutlar</DialogTitle>
        <DialogDescription className="sr-only">Sayfa, proje ya da hata kimliği arayın</DialogDescription>
        <Command
          loop
          className="[&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-item]]:h-9 [&_[cmdk-item]]:gap-2.5 [&_[cmdk-item]]:px-2.5 [&_[cmdk-item]]:text-md"
        >
          <CommandInput
            value={value}
            onValueChange={setValue}
            placeholder="Sayfa, proje ya da KALEM-1A gibi bir hata kimliği yazın"
            className="h-12 text-md"
          />
          <CommandList className="max-h-[min(60vh,420px)] p-1.5">
            <CommandEmpty className="py-8 text-center text-muted-foreground text-sm">Sonuç yok.</CommandEmpty>
            {text ? (
              <CommandGroup heading="Hatalar">
                {shortIdPattern.test(text) ? (
                  <CommandItem
                    value={`open ${text}`}
                    onSelect={() =>
                      run(() =>
                        navigate({
                          to: "/o/$org/issues/$issueId",
                          params: { org: org.slug, issueId: text.toUpperCase() },
                        }),
                      )
                    }
                  >
                    <CornerDownLeftIcon className="size-4 text-muted-foreground" />
                    <span className="font-mono">{text.toUpperCase()}</span> hatasını aç
                  </CommandItem>
                ) : null}
                <CommandItem
                  value={`search ${text}`}
                  onSelect={() =>
                    run(() =>
                      navigate({ to: "/o/$org/issues", params: { org: org.slug }, search: { query: text } }),
                    )
                  }
                >
                  <SearchIcon className="size-4 text-muted-foreground" />
                  Hatalarda ara: <span className="truncate text-muted-foreground">{text}</span>
                </CommandItem>
              </CommandGroup>
            ) : null}
            <CommandGroup heading="Sayfalar">
              {primaryNav.map((item) => (
                <CommandItem
                  key={item.to}
                  value={item.label}
                  onSelect={() => run(() => navigate({ to: item.to, params: { org: org.slug } }))}
                >
                  <item.icon className="size-4 text-muted-foreground" />
                  {item.label}
                </CommandItem>
              ))}
              {canAdmin(org.role) ? (
                <CommandItem
                  value="Organizasyon ayarları üyeler"
                  onSelect={() => run(() => navigate({ to: "/o/$org/settings", params: { org: org.slug } }))}
                >
                  <SettingsIcon className="size-4 text-muted-foreground" />
                  Organizasyon ayarları
                </CommandItem>
              ) : null}
              <CommandItem
                value="Hesabım profil parola oturumlar"
                onSelect={() => run(() => navigate({ to: "/o/$org/account", params: { org: org.slug } }))}
              >
                <UserIcon className="size-4 text-muted-foreground" />
                Hesabım
              </CommandItem>
              <CommandItem
                value="Erişim anahtarları token MCP"
                onSelect={() =>
                  run(() =>
                    navigate({ to: "/o/$org/account", params: { org: org.slug }, search: { tab: "tokens" } }),
                  )
                }
              >
                <KeyRoundIcon className="size-4 text-muted-foreground" />
                Erişim anahtarları
              </CommandItem>
            </CommandGroup>
            {projects.data?.projects.length ? (
              <CommandGroup heading="Projeler">
                {projects.data.projects.map((p) => (
                  <CommandItem
                    key={p.id}
                    value={`proje ${p.name} ${p.slug}`}
                    onSelect={() =>
                      run(() =>
                        navigate({ to: "/o/$org/projects/$slug", params: { org: org.slug, slug: p.slug } }),
                      )
                    }
                  >
                    <BoxIcon className="size-4 text-muted-foreground" />
                    {p.name}
                    <span className="ml-auto font-mono text-muted-foreground text-xs">{p.slug}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            ) : null}
            <CommandSeparator className="my-1" />
            <CommandGroup heading="Tema">
              <CommandItem value="Koyu tema" onSelect={() => run(() => setTheme("dark"))}>
                <MoonIcon className="size-4 text-muted-foreground" />
                Koyu tema
              </CommandItem>
              <CommandItem value="Açık tema" onSelect={() => run(() => setTheme("light"))}>
                <SunIcon className="size-4 text-muted-foreground" />
                Açık tema
              </CommandItem>
              <CommandItem value="Sistem teması" onSelect={() => run(() => setTheme("system"))}>
                <MonitorIcon className="size-4 text-muted-foreground" />
                Sistem teması
              </CommandItem>
              <CommandItem
                value="Görünümü özelleştir yazı tipi renk yoğunluk"
                onSelect={() => run(() => appearanceFlag.set(true))}
              >
                <PaletteIcon className="size-4 text-muted-foreground" />
                Görünümü özelleştir
              </CommandItem>
            </CommandGroup>
            <CommandGroup heading="Oturum">
              <CommandItem
                value="Çıkış yap"
                onSelect={() =>
                  run(async () => {
                    try {
                      await createClient(AuthService, baseTransport).logout({});
                    } finally {
                      qc.clear();
                      void navigate({ to: "/login" });
                    }
                  })
                }
              >
                <LogOutIcon className="size-4 text-muted-foreground" />
                Çıkış yap
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
