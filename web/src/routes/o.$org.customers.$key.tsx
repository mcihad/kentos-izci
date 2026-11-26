import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { StackedBars } from "@kentos/components/charts/stacked-bars";
import { Field } from "@kentos/components/common/field";
import { StatStrip } from "@kentos/components/common/stat-strip";
import { PageAction, PageBreadcrumb } from "@kentos/components/layout/page";
import { cn } from "@kentos/components/lib/utils";
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
import { Skeleton } from "@kentos/components/ui/skeleton";
import { Textarea } from "@kentos/components/ui/textarea";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { BugIcon, Building2Icon, FileTextIcon, PencilIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { PeriodSelect, parsePeriod } from "@/components/app/filters";
import { gutter, Page, Section, useDocumentTitle } from "@/components/app/page";
import { Sparkline } from "@/components/charts/sparkline";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { HealthLabel } from "@/components/customer/health";
import { IssuePreviewHost } from "@/components/issue/issue-preview";
import { IssueRow } from "@/components/issue/issue-row";
import { type Customer, CustomerService } from "@/gen/izci/api/v1/customer_pb";
import { errorMessage, isNotFound } from "@/lib/errors";
import { formatCompact, formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { seriesPoints, seriesStep } from "@/lib/series";
import { canWrite } from "@/lib/session";

export const Route = createFileRoute("/o/$org/customers/$key")({
  validateSearch: (s: Record<string, unknown>): { period?: string } => ({
    period: typeof s.period === "string" ? s.period : undefined,
  }),
  component: CustomerPage,
});

const periodTexts: Record<string, string> = {
  "1h": "son 1 saat",
  "24h": "son 24 saat",
  "7d": "son 7 gün",
  "14d": "son 14 gün",
  "30d": "son 30 gün",
  "90d": "son 90 gün",
};

function CustomerPage() {
  const org = useOrg();
  const { key } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const period = parsePeriod(search.period, "7d");
  const periodText = periodTexts[period.key] ?? "";
  const q = useQuery(CustomerService.method.getCustomer, { key, period: period.value });
  const [editing, setEditing] = useState(false);
  const d = q.data;
  const c = d?.summary?.customer;
  useDocumentTitle(c ? c.name || c.key : undefined);

  if (q.isPending) {
    return (
      <div className={cn("space-y-4 py-6", gutter)}>
        <Skeleton className="h-7 w-64" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-52 w-full" />
      </div>
    );
  }
  if (q.error || !d?.summary || !c) {
    return isNotFound(q.error) ? (
      <EmptyState title="Müşteri bulunamadı" className="py-24">
        <span className="font-mono">{key}</span> anahtarlı bir müşteriden hiç olay gelmedi.
      </EmptyState>
    ) : (
      <ErrorState error={q.error} onRetry={() => void q.refetch()} className="py-24" />
    );
  }
  const s = d.summary;

  return (
    <IssuePreviewHost writable={canWrite(org.role)}>
      <Page
        icon={Building2Icon}
        title={c.name || c.key}
        breadcrumb={
          <PageBreadcrumb>
            <Link to="/o/$org/customers" params={{ org: org.slug }}>
              Müşteriler
            </Link>
            <span className="font-mono">{c.key}</span>
          </PageBreadcrumb>
        }
        description={
          <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
            <HealthLabel health={s.health} className="text-foreground" />
            <span>
              İlk olay <RelativeTime value={c.firstSeen} />
            </span>
            <span>
              Son olay <RelativeTime value={c.lastSeen} />
            </span>
          </span>
        }
        filters={
          <PeriodSelect
            value={period.key}
            onChange={(k) => void navigate({ search: { period: k === "7d" ? undefined : k }, replace: true })}
          />
        }
        actions={
          <>
            {canWrite(org.role) ? (
              <PageAction icon={PencilIcon} label="Düzenle" onClick={() => setEditing(true)} />
            ) : null}
            <PageAction icon={BugIcon} label="Hatalarını gör" asChild>
              <Link
                to="/o/$org/issues"
                params={{ org: org.slug }}
                search={{ query: `is:unresolved customer:${c.key}` }}
              />
            </PageAction>
            <PageAction
              icon={FileTextIcon}
              label="Sağlık raporu"
              description="Yazdırılabilir rapor"
              variant="default"
              asChild
            >
              <Link
                to="/o/$org/customer-report/$key"
                params={{ org: org.slug, key: c.key }}
                search={{ period: search.period ?? "30d" }}
              />
            </PageAction>
          </>
        }
      >
        <div className={cn("space-y-9 pt-2", gutter)}>
          <StatStrip
            stats={[
              { label: "Açık hata", value: formatNumber(s.unresolvedIssues) },
              { label: "Yeni hata", value: formatNumber(s.newIssues), note: periodText },
              { label: "Olay", value: formatCompact(s.periodEvents), note: periodText },
              { label: "Etkilenen proje", value: formatNumber(s.affectedProjects) },
            ]}
          />
          <Section title="Olaylar">
            <StackedBars
              height={170}
              step={seriesStep(s.series)}
              layers={[
                { key: "events", label: "Olay", color: "var(--primary)", points: seriesPoints(s.series) },
              ]}
            />
          </Section>
          <div className="grid grid-cols-1 gap-9 xl:grid-cols-[minmax(0,1fr)_22rem]">
            <Section title="Açık hatalar" description="Bu müşteriden en çok olay gelen çözülmemiş hatalar">
              {d.topIssues.length === 0 ? (
                <p className="card px-4 py-8 text-center text-sm text-muted-foreground">Açık hata yok.</p>
              ) : (
                <div className="overflow-hidden card [&>div:last-child]:border-b-0">
                  {d.topIssues.map((i) => (
                    <IssueRow key={i.id} issue={i} org={org.slug} compact showUsers={false} />
                  ))}
                </div>
              )}
            </Section>
            <div className="space-y-9">
              <Section title="Projelere göre">
                <ul className="divide-y card">
                  {d.projects.map((p) => (
                    <li key={p.project?.id} className="flex items-center gap-3 px-3 py-2.5 text-sm">
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-medium">{p.project?.name}</span>
                        <span className="text-xs text-muted-foreground tabular">
                          {formatNumber(p.unresolvedIssues)} açık hata
                        </span>
                      </span>
                      <Sparkline series={p.series} className="h-5 w-20" />
                      <span className="w-12 text-right tabular">{formatCompact(p.periodEvents)}</span>
                    </li>
                  ))}
                </ul>
              </Section>
              {c.notes ? (
                <Section title="Notlar">
                  <p className="whitespace-pre-wrap card px-4 py-3 text-sm leading-relaxed">{c.notes}</p>
                </Section>
              ) : null}
              {Object.keys(c.attributes).length > 0 ? (
                <Section title="Uygulamanın bildirdiği bilgiler">
                  <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-5 gap-y-1.5 text-sm">
                    {Object.entries(c.attributes).map(([k, v]) => (
                      <div key={k} className="contents">
                        <dt className="text-muted-foreground">{k}</dt>
                        <dd className="wrap-anywhere">{v}</dd>
                      </div>
                    ))}
                  </dl>
                </Section>
              ) : null}
            </div>
          </div>
        </div>
        <EditCustomer customer={c} open={editing} onOpenChange={setEditing} />
      </Page>
    </IssuePreviewHost>
  );
}

function EditCustomer({
  customer,
  open,
  onOpenChange,
}: {
  customer: Customer;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const org = useOrg();
  const qc = useQueryClient();
  const [name, setName] = useState(customer.name);
  const [notes, setNotes] = useState(customer.notes);
  const update = useMutation(CustomerService.method.updateCustomer, {
    onSuccess: () => {
      toast.success("Müşteri bilgileri kaydedildi");
      onOpenChange(false);
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: CustomerService,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Müşteriyi düzenle</DialogTitle>
          <DialogDescription>
            Burada verdiğiniz ad, uygulamaların gönderdiği addan önceliklidir.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            update.mutate({ key: customer.key, name, notes });
          }}
        >
          <Field label="Görünen ad" htmlFor="customer-name">
            <Input id="customer-name" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field
            label="Notlar"
            htmlFor="customer-notes"
            hint="Sözleşme, iletişim kişisi, özel kurulum bilgileri…"
          >
            <Textarea
              id="customer-notes"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              className="min-h-28"
            />
          </Field>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={update.isPending}>
              Kaydet
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
