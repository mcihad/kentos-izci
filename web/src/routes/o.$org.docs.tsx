import { createFileRoute, Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { gutter, PageHeader } from "@/components/app/page";
import { sections, useGuides } from "@/components/docs/docs";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useOrg } from "@/lib/org";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/o/$org/docs")({
  component: DocsLayout,
});

function DocsLayout() {
  const org = useOrg();
  const guides = useGuides();
  const params = useParams({ strict: false }) as { _splat?: string };
  const active = params._splat ?? "";
  const navigate = useNavigate();
  const groups = sections(guides.data ?? []);

  return (
    <div className="pb-16">
      <PageHeader title="Belgeler" description="Kurulum, veri gönderme, kullanım ve yönetim rehberleri" />
      <div className={cn("grid grid-cols-1 gap-8 pt-6 lg:grid-cols-[14rem_minmax(0,1fr)]", gutter)}>
        <div className="lg:hidden">
          <Select
            value={active || "index"}
            onValueChange={(slug) =>
              void navigate({
                to: slug === "index" ? "/o/$org/docs" : "/o/$org/docs/$",
                params: { org: org.slug, _splat: slug },
              })
            }
          >
            <SelectTrigger className="w-full" aria-label="Rehber">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="index">Bütün rehberler</SelectItem>
              {groups.map((s) => (
                <SelectGroup key={s.name}>
                  <SelectLabel>{s.name}</SelectLabel>
                  {s.guides.map((g) => (
                    <SelectItem key={g.slug} value={g.slug}>
                      {g.title}
                    </SelectItem>
                  ))}
                </SelectGroup>
              ))}
            </SelectContent>
          </Select>
        </div>
        <nav aria-label="Rehberler" className="hidden lg:block">
          <div className="sticky top-6 grid gap-5">
            {groups.map((s) => (
              <div key={s.name}>
                <p className="mb-1.5 px-2 font-medium text-[12px] text-muted-foreground">{s.name}</p>
                <ul className="grid gap-px">
                  {s.guides.map((g) => (
                    <li key={g.slug}>
                      <Link
                        to="/o/$org/docs/$"
                        params={{ org: org.slug, _splat: g.slug }}
                        className={cn(
                          "block rounded-md px-2 py-1 text-[13px] transition-colors",
                          g.slug === active
                            ? "bg-accent font-medium text-foreground"
                            : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                        )}
                      >
                        {g.title}
                      </Link>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </nav>
        <div className="min-w-0">
          <Outlet />
        </div>
      </div>
    </div>
  );
}
