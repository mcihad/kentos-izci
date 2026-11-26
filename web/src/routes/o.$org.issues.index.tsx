import { useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import {
  BugIcon,
  CheckIcon,
  EyeOffIcon,
  MergeIcon,
  SearchXIcon,
  UserRoundCheckIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { PeriodSelect, ProjectFilter, parsePeriod } from "@/components/app/filters";
import { gutter, PageHeader } from "@/components/app/page";
import { EmptyState, ErrorState } from "@/components/common/states";
import { TabStrip, tabActiveOptions, tabClass } from "@/components/common/tab-nav";
import { IssueRow } from "@/components/issue/issue-row";
import { MergeDialog } from "@/components/issue/merge";
import { ExportCsvButton, SavedSearchMenu } from "@/components/issue/saved-searches";
import { SearchBar, type SearchBarHandle } from "@/components/issue/search-bar";
import { ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { IssueService, IssueSort } from "@/gen/izci/api/v1/issue_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorReason } from "@/lib/errors";
import { formatNumber } from "@/lib/format";
import { periodKey } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { canWrite } from "@/lib/session";
import { useHotkeys } from "@/lib/shortcuts";
import { useTriage } from "@/lib/triage";

// Plain keys for URL validation: route definitions stay in the entry chunk,
// so they must not pull in generated message code.
const sortKeys = ["last_seen", "first_seen", "events", "users", "priority"] as const;
type SortKey = (typeof sortKeys)[number];

const sorts: Record<SortKey, { value: IssueSort; label: string }> = {
  last_seen: { value: IssueSort.LAST_SEEN, label: "Son görülme" },
  first_seen: { value: IssueSort.FIRST_SEEN, label: "İlk görülme" },
  events: { value: IssueSort.EVENTS, label: "Olay sayısı" },
  users: { value: IssueSort.USERS, label: "Kullanıcı sayısı" },
  priority: { value: IssueSort.PRIORITY, label: "Öncelik" },
};

type IssuesSearch = { query?: string; sort?: SortKey; period?: string; projects?: string };

const defaultQuery = "is:unresolved";

/** Saved views shown as tabs above the search bar. */
const views = [
  { label: "Çözülmemiş", query: "is:unresolved" },
  { label: "Bana atanan", query: "is:unresolved assigned:me" },
  { label: "Yeni", query: "is:unresolved is:new" },
  { label: "Geri dönen", query: "is:regressed" },
  { label: "Çözülmüş", query: "is:resolved" },
  { label: "Yoksayılan", query: "is:ignored" },
];

export const Route = createFileRoute("/o/$org/issues/")({
  validateSearch: (s: Record<string, unknown>): IssuesSearch => ({
    query: typeof s.query === "string" ? s.query : undefined,
    sort: sortKeys.find((k) => k === s.sort),
    period: typeof s.period === "string" ? s.period : undefined,
    projects: typeof s.projects === "string" && s.projects ? s.projects : undefined,
  }),
  component: IssuesPage,
});

function IssuesPage() {
  const org = useOrg();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const query = search.query ?? defaultQuery;
  const sort = search.sort ?? "last_seen";
  const period = parsePeriod(search.period, "14d");
  const projects = useMemo(() => (search.projects ? search.projects.split(",") : []), [search.projects]);
  const searchRef = useRef<SearchBarHandle>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const triage = useTriage();
  const writable = canWrite(org.role);

  const issues = useInfiniteQuery(
    IssueService.method.listIssues,
    { query, sort: sorts[sort].value, statsPeriod: period.value, projects, pageSize: 50, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const rows = useMemo(() => issues.data?.pages.flatMap((p) => p.issues) ?? [], [issues.data]);
  const total = issues.data?.pages[0]?.totalEstimate ?? 0;

  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [merging, setMerging] = useState(false);
  const [focus, setFocus] = useState(-1);
  // A new result set clears selection and keyboard focus.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset when the filters change
  useEffect(() => {
    setSelected(new Set());
    setFocus(-1);
  }, [query, sort, period.key, search.projects]);

  const virtualizer = useWindowVirtualizer({
    count: rows.length,
    estimateSize: () => 78,
    overscan: 8,
    scrollMargin: listRef.current?.offsetTop ?? 0,
  });
  const items = virtualizer.getVirtualItems();
  const last = items[items.length - 1];
  useEffect(() => {
    if (last && last.index >= rows.length - 8 && issues.hasNextPage && !issues.isFetchingNextPage) {
      void issues.fetchNextPage();
    }
  }, [last, rows.length, issues]);

  const setSearch = (patch: Partial<IssuesSearch>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  const targets = () => {
    if (selected.size > 0) return [...selected];
    const f = rows[focus];
    return f ? [f.id] : [];
  };
  const moveFocus = (to: number) => {
    const i = Math.max(0, Math.min(rows.length - 1, to));
    setFocus(i);
    virtualizer.scrollToIndex(i, { align: "auto" });
  };
  const openFocused = () => {
    const f = rows[focus];
    if (f) void navigate({ to: "/o/$org/issues/$issueId", params: { org: org.slug, issueId: f.shortId } });
  };
  useHotkeys({
    "/": () => searchRef.current?.focus(),
    j: () => moveFocus(focus + 1),
    k: () => moveFocus(focus - 1),
    Enter: openFocused,
    x: () => {
      const f = rows[focus];
      if (!f) return;
      setSelected((s) => {
        const n = new Set(s);
        if (n.has(f.id)) n.delete(f.id);
        else n.add(f.id);
        return n;
      });
    },
    r: () =>
      writable && targets().length && void triage.resolve(targets()).then(() => setSelected(new Set())),
    i: () => writable && targets().length && void triage.ignore(targets()).then(() => setSelected(new Set())),
    a: () => writable && targets().length && void triage.assign(targets(), triage.me),
  });

  const activeView = views.find((v) => v.query === query.trim().replace(/\s+/g, " "));
  const invalidQuery = issues.error && errorReason(issues.error) === ErrorReason.INVALID_QUERY;

  return (
    <div className="pb-24">
      <PageHeader
        title="Hatalar"
        description={
          issues.isPending ? (
            "Yükleniyor…"
          ) : (
            <span className="tabular">
              {formatNumber(total)}
              {total >= 10000 ? "+" : ""} hata,{" "}
              {period.value ? `${periodTitle(period.key)} içinde görülen` : ""}
            </span>
          )
        }
      />

      <div className={gutter}>
        {/* The design system's list card: views, search and filters, then the rows.
            On phones it runs edge to edge to give the rows the full width. */}
        <div className="card max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
          <TabStrip label="Görünümler" active={activeView?.label ?? ""} className="px-2 sm:px-3">
            {views.map((v) => (
              <Link
                key={v.label}
                from={Route.fullPath}
                search={(prev) => ({ ...prev, query: v.query === defaultQuery ? undefined : v.query })}
                activeOptions={tabActiveOptions}
                className={tabClass(activeView === v)}
                aria-current={activeView === v ? "page" : undefined}
              >
                {v.label}
              </Link>
            ))}
          </TabStrip>

          <div className="flex flex-col gap-2.5 border-b px-4 py-3 md:flex-row md:items-center">
            <SearchBar
              ref={searchRef}
              value={query}
              onSubmit={(q) => setSearch({ query: q === defaultQuery ? undefined : q || undefined })}
              className="flex-1"
            />
            <div className="scrollbar-none -mx-4 flex items-center gap-2 overflow-x-auto px-4 *:shrink-0 md:mx-0 md:overflow-visible md:px-0">
              <ProjectFilter
                value={projects}
                onChange={(p) => setSearch({ projects: p.length ? p.join(",") : undefined })}
              />
              <Select
                value={sort}
                onValueChange={(v) => setSearch({ sort: v === "last_seen" ? undefined : (v as SortKey) })}
              >
                <SelectTrigger size="sm" className="text-sm" aria-label="Sıralama">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="end">
                  {Object.entries(sorts).map(([k, s]) => (
                    <SelectItem key={k} value={k}>
                      {s.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <PeriodSelect
                value={period.key}
                onChange={(k) => setSearch({ period: k === "14d" ? undefined : k })}
              />
              <SavedSearchMenu
                current={{ query, sort: sorts[sort].value, period: period.value, projects }}
                onApply={(s) => {
                  const key = sortKeys.find((k) => sorts[k].value === s.sort) ?? "last_seen";
                  const p = s.statsPeriod ? periodKey(s.statsPeriod) : "14d";
                  setSearch({
                    query: s.query === defaultQuery || !s.query ? undefined : s.query,
                    sort: key === "last_seen" ? undefined : key,
                    period: p === "14d" ? undefined : p,
                    projects: s.projects.length ? s.projects.join(",") : undefined,
                  });
                }}
              />
              <ExportCsvButton current={{ query, sort: sorts[sort].value, period: period.value, projects }} />
            </div>
          </div>

          {invalidQuery ? (
            <p className="border-b px-4 py-2.5 text-destructive text-sm">
              Arama ifadesi anlaşılamadı: {issues.error?.rawMessage}
            </p>
          ) : null}

          {issues.isPending ? (
            <ListSkeleton />
          ) : issues.error && !invalidQuery ? (
            <ErrorState error={issues.error} onRetry={() => void issues.refetch()} />
          ) : rows.length === 0 ? (
            <NoIssues
              filtered={query !== defaultQuery || projects.length > 0}
              onClear={() => setSearch({ query: undefined, projects: undefined })}
            />
          ) : (
            <div ref={listRef} className="relative -mb-px" style={{ height: virtualizer.getTotalSize() }}>
              {items.map((v) => {
                const issue = rows[v.index];
                if (!issue) return null;
                return (
                  <div
                    key={issue.id}
                    data-index={v.index}
                    ref={virtualizer.measureElement}
                    className="absolute inset-x-0 top-0"
                    style={{ transform: `translateY(${v.start - virtualizer.options.scrollMargin}px)` }}
                  >
                    <IssueRow
                      issue={issue}
                      org={org.slug}
                      focused={focus === v.index}
                      selected={selected.has(issue.id)}
                      onSelect={
                        writable
                          ? (on) =>
                              setSelected((s) => {
                                const n = new Set(s);
                                if (on) n.add(issue.id);
                                else n.delete(issue.id);
                                return n;
                              })
                          : undefined
                      }
                    />
                  </div>
                );
              })}
            </div>
          )}
          {issues.isFetchingNextPage ? (
            <p className="border-t py-4 text-center text-muted-foreground text-sm">Yükleniyor…</p>
          ) : null}
        </div>
      </div>

      {selected.size > 0 ? (
        <div className="fixed inset-x-0 bottom-5 z-40 flex justify-center px-4">
          <div className="flex items-center gap-1 rounded-lg border bg-popover p-1.5 pl-3.5 shadow-xl">
            <span className="mr-2 text-sm tabular">{selected.size} hata seçildi</span>
            <Button
              size="sm"
              variant="ghost"
              disabled={triage.pending}
              onClick={() => void triage.resolve([...selected]).then(() => setSelected(new Set()))}
            >
              <CheckIcon className="size-4" />
              Çöz
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={triage.pending}
              onClick={() => void triage.ignore([...selected]).then(() => setSelected(new Set()))}
            >
              <EyeOffIcon className="size-4" />
              Yoksay
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={triage.pending}
              onClick={() => void triage.assign([...selected], triage.me)}
            >
              <UserRoundCheckIcon className="size-4" />
              Bana ata
            </Button>
            {selected.size > 1 ? (
              <Button size="sm" variant="ghost" disabled={triage.pending} onClick={() => setMerging(true)}>
                <MergeIcon className="size-4" />
                Birleştir
              </Button>
            ) : null}
            <Button
              size="icon"
              variant="ghost"
              className="size-8"
              aria-label="Seçimi temizle"
              onClick={() => setSelected(new Set())}
            >
              <XIcon className="size-4" />
            </Button>
          </div>
        </div>
      ) : null}
      {merging ? (
        <MergeDialog
          issues={rows.filter((i) => selected.has(i.id))}
          open={merging}
          onOpenChange={setMerging}
          onMerged={() => setSelected(new Set())}
        />
      ) : null}
    </div>
  );
}

function periodTitle(key: string): string {
  return (
    {
      "1h": "son 1 saat",
      "24h": "son 24 saat",
      "7d": "son 7 gün",
      "14d": "son 14 gün",
      "30d": "son 30 gün",
      "90d": "son 90 gün",
    }[key] ?? ""
  );
}

function ListSkeleton() {
  return (
    <div>
      {Array.from({ length: 8 }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: static placeholders
        <div key={i} className={cn("flex gap-3 border-b py-3.5", gutter)}>
          <Skeleton className="h-11 w-[3px]" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3 w-1/3" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  );
}

function NoIssues({ filtered, onClear }: { filtered: boolean; onClear: () => void }) {
  const org = useOrg();
  const projects = useQuery(ProjectService.method.listProjects, {}, { staleTime: 60_000 });
  const silent = projects.data?.projects.every((p) => !p.stats?.lastEventAt);
  if (filtered) {
    return (
      <EmptyState
        icon={SearchXIcon}
        title="Aramanızla eşleşen hata yok"
        action={
          <Button variant="outline" size="sm" onClick={onClear}>
            Filtreleri temizle
          </Button>
        }
      >
        Farklı bir dönem seçin ya da arama ifadesini genişletin.
      </EmptyState>
    );
  }
  if (silent) {
    const first = projects.data?.projects[0];
    return (
      <EmptyState
        icon={BugIcon}
        title="Henüz hiç olay gelmedi"
        action={
          first ? (
            <Button asChild size="sm">
              <Link
                to="/o/$org/projects/$slug"
                params={{ org: org.slug, slug: first.slug }}
                search={{ tab: "setup" }}
              >
                Kurulum yönergelerini aç
              </Link>
            </Button>
          ) : null
        }
      >
        Uygulamanıza İzci'yi bağladığınızda hatalar burada gruplanarak listelenir.
      </EmptyState>
    );
  }
  return (
    <EmptyState icon={CheckIcon} title="Çözülmemiş hata yok">
      Bu dönemde yeni ya da süren bir hata görülmedi.
    </EmptyState>
  );
}
