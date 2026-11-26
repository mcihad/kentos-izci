import { create } from "@bufbuild/protobuf";
import { useQuery } from "@connectrpc/connect-query";
import { StackedBars } from "@kentos/components/charts/stacked-bars";
import { Field } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Input } from "@kentos/components/ui/input";
import { Label } from "@kentos/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@kentos/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { Link } from "@tanstack/react-router";
import { ArrowDownIcon, ArrowUpIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { IssueRow } from "@/components/issue/issue-row";
import { StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { IssueService, IssueSort } from "@/gen/izci/api/v1/issue_pb";
import {
  type DashboardWidget,
  DashboardWidgetKind,
  DashboardWidgetSchema,
  ViewService,
} from "@/gen/izci/api/v1/view_pb";
import { errorMessage } from "@/lib/errors";
import { formatCompact, formatNumber } from "@/lib/format";
import { periodKey, periods } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { seriesPoints, seriesStep } from "@/lib/series";

const sortOptions = [
  { value: IssueSort.EVENTS, label: "Olay sayısı" },
  { value: IssueSort.LAST_SEEN, label: "Son görülme" },
  { value: IssueSort.FIRST_SEEN, label: "İlk görülme" },
  { value: IssueSort.USERS, label: "Kullanıcı sayısı" },
  { value: IssueSort.PRIORITY, label: "Öncelik" },
];

const widgetPeriods = periods.filter((p) => p.value !== StatsPeriod.LAST_HOUR);

function periodLabel(p: StatsPeriod): string {
  return (widgetPeriods.find((x) => x.value === p) ?? widgetPeriods[0])?.label ?? "";
}

/** One panel of a dashboard. */
export function Widget({
  widget,
  editable,
  onRemove,
  onMove,
}: {
  widget: DashboardWidget;
  editable: boolean;
  onRemove: () => void;
  onMove: (by: -1 | 1) => void;
}) {
  const org = useOrg();
  const period = widget.statsPeriod || StatsPeriod.LAST_24_HOURS;
  return (
    <section className="card flex min-w-0 flex-col">
      <header className="flex items-start gap-2 border-b px-4 py-3">
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-semibold text-md">
            {widget.title || widget.query || "Çözülmemiş hatalar"}
          </h2>
          <p className="truncate font-mono text-muted-foreground text-xs">
            {widget.query || "is:unresolved"} · {periodLabel(period).toLocaleLowerCase("tr")}
          </p>
        </div>
        {editable ? (
          <div className="-mr-1 flex shrink-0">
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label="Yukarı taşı"
              onClick={() => onMove(-1)}
            >
              <ArrowUpIcon className="size-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label="Aşağı taşı"
              onClick={() => onMove(1)}
            >
              <ArrowDownIcon className="size-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label="Bileşeni kaldır"
              onClick={onRemove}
            >
              <Trash2Icon className="size-3.5" />
            </Button>
          </div>
        ) : null}
      </header>
      <div className="min-w-0 flex-1">
        {widget.kind === DashboardWidgetKind.CHART ? (
          <ChartBody widget={widget} period={period} />
        ) : (
          <IssuesBody widget={widget} period={period} />
        )}
      </div>
      <footer className="border-t px-4 py-2">
        <Link
          to="/o/$org/issues"
          params={{ org: org.slug }}
          search={{
            query: widget.query || undefined,
            period: periodKey(period),
            projects: widget.projects.length ? widget.projects.join(",") : undefined,
          }}
          className="text-link text-sm underline-offset-4 hover:underline"
        >
          Hatalarda aç
        </Link>
      </footer>
    </section>
  );
}

function ChartBody({ widget, period }: { widget: DashboardWidget; period: StatsPeriod }) {
  const q = useQuery(IssueService.method.getQueryStats, {
    query: widget.query,
    projects: widget.projects,
    statsPeriod: period,
  });
  if (q.isPending) return <Skeleton className="m-4 h-40" />;
  if (q.error) return <p className="px-4 py-6 text-destructive text-sm">{errorMessage(q.error)}</p>;
  const s = q.data;
  return (
    <div className="grid gap-3 px-4 py-4">
      <dl className="flex gap-8">
        <div>
          <dt className="text-muted-foreground text-xs">Hata</dt>
          <dd className="font-semibold text-2xl tabular">
            {formatNumber(s.issues)}
            {s.issues >= 10000 ? "+" : ""}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground text-xs">Olay</dt>
          <dd className="font-semibold text-2xl tabular">{formatCompact(s.events)}</dd>
        </div>
      </dl>
      <StackedBars
        height={120}
        step={seriesStep(s.series)}
        layers={[{ key: "events", label: "Olay", color: "var(--primary)", points: seriesPoints(s.series) }]}
      />
    </div>
  );
}

function IssuesBody({ widget, period }: { widget: DashboardWidget; period: StatsPeriod }) {
  const org = useOrg();
  const q = useQuery(IssueService.method.listIssues, {
    query: widget.query,
    projects: widget.projects,
    statsPeriod: period,
    sort: widget.sort || IssueSort.EVENTS,
    pageSize: widget.limit || 5,
  });
  if (q.isPending) return <Skeleton className="m-4 h-40" />;
  if (q.error) return <p className="px-4 py-6 text-destructive text-sm">{errorMessage(q.error)}</p>;
  if (q.data.issues.length === 0)
    return <p className="px-4 py-8 text-center text-muted-foreground text-sm">Eşleşen hata yok.</p>;
  return (
    <div className="[&>div:last-child]:border-b-0">
      {q.data.issues.map((i) => (
        <IssueRow key={i.id} issue={i} org={org.slug} compact showUsers={false} />
      ))}
    </div>
  );
}

/** Adds a widget: a search shown as a chart or a list, optionally from a saved search. */
export function AddWidgetDialog({
  open,
  onOpenChange,
  onAdd,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onAdd: (w: DashboardWidget) => void;
}) {
  const saved = useQuery(ViewService.method.listSavedSearches, {}, { enabled: open, staleTime: 60_000 });
  const [kind, setKind] = useState(DashboardWidgetKind.CHART);
  const [title, setTitle] = useState("");
  const [query, setQuery] = useState("is:unresolved");
  const [projects, setProjects] = useState<string[]>([]);
  const [period, setPeriod] = useState(StatsPeriod.LAST_7_DAYS);
  const [sort, setSort] = useState(IssueSort.EVENTS);
  const [limit, setLimit] = useState("5");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Bileşen ekle</DialogTitle>
          <DialogDescription>Bir aramayı grafik ya da hata listesi olarak gösterin.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            onAdd(
              create(DashboardWidgetSchema, {
                kind,
                title: title.trim(),
                query: query.trim(),
                projects,
                statsPeriod: period,
                sort,
                limit: Math.min(20, Math.max(1, Math.trunc(Number(limit) || 5))),
              }),
            );
            onOpenChange(false);
            setTitle("");
          }}
        >
          <RadioGroup
            value={String(kind)}
            onValueChange={(v) => setKind(Number(v) as DashboardWidgetKind)}
            className="flex gap-5"
          >
            <div className="flex items-center gap-2">
              <RadioGroupItem value={String(DashboardWidgetKind.CHART)} id="w-chart" />
              <Label htmlFor="w-chart" className="font-normal">
                Grafik
              </Label>
            </div>
            <div className="flex items-center gap-2">
              <RadioGroupItem value={String(DashboardWidgetKind.ISSUES)} id="w-issues" />
              <Label htmlFor="w-issues" className="font-normal">
                Hata listesi
              </Label>
            </div>
          </RadioGroup>
          {saved.data?.searches.length ? (
            <Field label="Kayıtlı aramadan doldur">
              <Select
                onValueChange={(id) => {
                  const s = saved.data?.searches.find((x) => x.id === id);
                  if (!s) return;
                  setTitle(s.name);
                  setQuery(s.query || "is:unresolved");
                  setProjects(s.projects);
                  if (s.statsPeriod && s.statsPeriod !== StatsPeriod.LAST_HOUR) setPeriod(s.statsPeriod);
                  if (s.sort) setSort(s.sort);
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Bir kayıtlı arama seçin" />
                </SelectTrigger>
                <SelectContent>
                  {saved.data.searches.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          <Field label="Başlık" htmlFor="w-title">
            <Input id="w-title" maxLength={100} value={title} onChange={(e) => setTitle(e.target.value)} />
          </Field>
          <Field
            label="Arama"
            htmlFor="w-query"
            hint="Hata listesindeki arama ifadesi, örneğin is:unresolved level:fatal customer:sivas-belediyesi"
          >
            <Input
              id="w-query"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="font-mono text-sm"
            />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Dönem">
              <Select value={String(period)} onValueChange={(v) => setPeriod(Number(v) as StatsPeriod)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {widgetPeriods.map((p) => (
                    <SelectItem key={p.value} value={String(p.value)}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            {kind === DashboardWidgetKind.ISSUES ? (
              <Field label="Sıralama">
                <Select value={String(sort)} onValueChange={(v) => setSort(Number(v) as IssueSort)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {sortOptions.map((o) => (
                      <SelectItem key={o.value} value={String(o.value)}>
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            ) : null}
          </div>
          {kind === DashboardWidgetKind.ISSUES ? (
            <Field label="Gösterilecek hata" htmlFor="w-limit">
              <Input
                id="w-limit"
                type="number"
                min={1}
                max={20}
                value={limit}
                onChange={(e) => setLimit(e.target.value)}
                className="w-24"
              />
            </Field>
          ) : null}
          {projects.length > 0 ? (
            <p className="text-muted-foreground text-xs">Projeler: {projects.join(", ")}</p>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit">Ekle</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
