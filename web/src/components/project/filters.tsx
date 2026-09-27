import { clone, create } from "@bufbuild/protobuf";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { StackedBars } from "@/components/charts/stacked-bars";
import { Field } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  FilterReason,
  InboundFiltersSchema,
  type Project,
  ProjectService,
  ProjectSettingsSchema,
} from "@/gen/izci/api/v1/project_pb";
import { formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { useUpdateProject } from "./use-project";

const reasonLabels: Record<FilterReason, string> = {
  [FilterReason.UNSPECIFIED]: "Diğer",
  [FilterReason.ENVIRONMENT]: "Kayıt dışı ortam",
  [FilterReason.BROWSER_EXTENSION]: "Tarayıcı eklentisi",
  [FilterReason.WEB_CRAWLER]: "Bot",
  [FilterReason.LEGACY_BROWSER]: "Eski tarayıcı",
  [FilterReason.LOCALHOST]: "Yerel geliştirme",
  [FilterReason.ERROR_MESSAGE]: "Hata mesajı",
  [FilterReason.IP_ADDRESS]: "IP adresi",
  [FilterReason.RELEASE]: "Sürüm",
};

type Toggle = "browserExtensions" | "webCrawlers" | "legacyBrowsers" | "localhost";

const toggles: { key: Toggle; label: string; hint: string }[] = [
  {
    key: "browserExtensions",
    label: "Tarayıcı eklentileri",
    hint: "Eklenti betiklerinden (chrome-extension://, moz-extension:// …) gelen hataları, eklentilerin bilinen hatalarını ve ayrıntısız “Script error.” hatalarını atar.",
  },
  {
    key: "webCrawlers",
    label: "Botlar",
    hint: "Arama motoru botlarından, bağlantı önizleyicilerinden ve çalışma süresi denetleyicilerinden (Googlebot, Bingbot, Pingdom, Lighthouse …) gelen olayları atar.",
  },
  {
    key: "legacyBrowsers",
    label: "Eski tarayıcılar",
    hint: "Internet Explorer, Edge 18, Safari 11, Chrome 79, Firefox 77, Opera 66 ve öncesi ile Opera Mini ve eski Android tarayıcısından gelen olayları atar.",
  },
  {
    key: "localhost",
    label: "Yerel geliştirme",
    hint: "localhost, 127.0.0.1 ya da ::1 adresindeki sayfalardan ve bu adreslerden gönderilen olayları atar.",
  },
];

const lines = (s: string) =>
  s
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean);

/** Inbound filters: what is dropped at ingest, and how much was. */
export function ProjectFilters({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const update = useUpdateProject();
  const f = project.settings?.filters;
  const [on, setOn] = useState<Record<Toggle, boolean>>({
    browserExtensions: f?.browserExtensions ?? false,
    webCrawlers: f?.webCrawlers ?? false,
    legacyBrowsers: f?.legacyBrowsers ?? false,
    localhost: f?.localhost ?? false,
  });
  const [environments, setEnvironments] = useState(project.settings?.ignoredEnvironments.join("\n") ?? "");
  const [messages, setMessages] = useState(f?.errorMessages.join("\n") ?? "");
  const [ips, setIps] = useState(f?.ipAddresses.join("\n") ?? "");
  const [releases, setReleases] = useState(f?.releases.join("\n") ?? "");
  const stats = useQuery(ProjectService.method.getFilterStats, { project: project.slug });

  const save = () => {
    const settings = clone(ProjectSettingsSchema, project.settings ?? create(ProjectSettingsSchema));
    settings.ignoredEnvironments = lines(environments.replaceAll(",", "\n")).map((e) => e.toLowerCase());
    settings.filters = create(InboundFiltersSchema, {
      ...on,
      errorMessages: lines(messages),
      ipAddresses: lines(ips),
      releases: lines(releases),
    });
    update.mutate({ slug: project.slug, settings });
  };

  // The last 30 days, with empty days filled in.
  const byDay = new Map((stats.data?.days ?? []).map((d) => [d.day, Number(d.count)]));
  const points = Array.from({ length: 30 }, (_, i) => {
    const t = new Date();
    t.setUTCHours(0, 0, 0, 0);
    t.setUTCDate(t.getUTCDate() - 29 + i);
    return { t, v: byDay.get(t.toISOString().slice(0, 10)) ?? 0 };
  });
  const total = Number(stats.data?.total ?? 0n);
  const max = Math.max(1, ...(stats.data?.reasons ?? []).map((r) => Number(r.count)));

  return (
    <div className="grid max-w-3xl grid-cols-1 *:min-w-0 gap-10 py-6">
      <section>
        <h2 className="font-semibold text-lg">Son 30 günde atılan olaylar</h2>
        <p className="mt-0.5 text-md text-muted-foreground">
          Filtrelere takılan olaylar saklanmaz; hız sınırına ve aylık kotaya da sayılmaz.
        </p>
        {stats.isPending ? (
          <Skeleton className="mt-4 h-28 w-full" />
        ) : total === 0 ? (
          <p className="mt-4 text-md">Son 30 günde filtrelere takılan olay yok.</p>
        ) : (
          <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <div>
              <p className="font-semibold text-2xl tabular">{formatNumber(total)}</p>
              <StackedBars
                layers={[{ key: "filtered", label: "Atılan", color: "var(--muted-foreground)", points }]}
                step={86_400_000}
                height={64}
              />
            </div>
            <ul className="grid content-start gap-2">
              {(stats.data?.reasons ?? []).map((r) => (
                <li key={r.reason} className="grid gap-1 text-sm">
                  <div className="flex justify-between gap-3">
                    <span>{reasonLabels[r.reason]}</span>
                    <span className="tabular text-muted-foreground">{formatNumber(r.count)}</span>
                  </div>
                  <div className="h-1.5 rounded-full bg-(--surface-muted)">
                    <div
                      className="h-full rounded-full bg-muted-foreground/60"
                      style={{ width: `${Math.max(2, (Number(r.count) / max) * 100)}%` }}
                    />
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>

      <form
        className="grid gap-8"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <section className="grid gap-4">
          <h2 className="font-semibold text-lg">Hazır filtreler</h2>
          {toggles.map((t) => (
            <div key={t.key} className="flex items-start gap-3">
              <Switch
                id={`filter-${t.key}`}
                checked={on[t.key]}
                disabled={!admin}
                onCheckedChange={(v) => setOn((s) => ({ ...s, [t.key]: v }))}
                className="mt-0.5"
              />
              <div className="grid gap-0.5">
                <Label htmlFor={`filter-${t.key}`} className="text-md">
                  {t.label}
                </Label>
                <p className="text-sm text-muted-foreground">{t.hint}</p>
              </div>
            </div>
          ))}
        </section>

        <section className="grid gap-5">
          <h2 className="font-semibold text-lg">Kendi filtreleriniz</h2>
          <Field
            label="Kayıt dışı ortamlar"
            htmlFor="filter-envs"
            hint="Her satıra bir ortam, örneğin development ya da test."
          >
            <Textarea
              id="filter-envs"
              rows={2}
              className="font-mono"
              value={environments}
              disabled={!admin}
              onChange={(e) => setEnvironments(e.target.value)}
            />
          </Field>
          <Field
            label="Hata mesajları"
            htmlFor="filter-messages"
            hint="Her satıra bir desen; * her şeyle eşleşir, büyük küçük harf fark etmez. Örnek: *ResizeObserver loop*"
          >
            <Textarea
              id="filter-messages"
              rows={4}
              className="font-mono"
              placeholder={"*ResizeObserver loop*\nChunkLoadError*"}
              value={messages}
              disabled={!admin}
              onChange={(e) => setMessages(e.target.value)}
            />
          </Field>
          <Field
            label="IP adresleri"
            htmlFor="filter-ips"
            hint="Her satıra bir adres ya da aralık (10.0.0.0/8). Hem son kullanıcının hem olayı gönderen sunucunun adresine bakılır."
          >
            <Textarea
              id="filter-ips"
              rows={3}
              className="font-mono"
              placeholder={"203.0.113.7\n10.20.0.0/16"}
              value={ips}
              disabled={!admin}
              onChange={(e) => setIps(e.target.value)}
            />
          </Field>
          <Field
            label="Sürümler"
            htmlFor="filter-releases"
            hint="Her satıra bir desen, örneğin geliştirme derlemeleri için *-dev."
          >
            <Textarea
              id="filter-releases"
              rows={3}
              className="font-mono"
              placeholder={"*-dev\nkalem-web@4.*"}
              value={releases}
              disabled={!admin}
              onChange={(e) => setReleases(e.target.value)}
            />
          </Field>
        </section>
        {admin ? (
          <div>
            <Button type="submit" disabled={update.isPending}>
              {update.isPending ? "Kaydediliyor…" : "Filtreleri kaydet"}
            </Button>
          </div>
        ) : null}
      </form>
    </div>
  );
}
