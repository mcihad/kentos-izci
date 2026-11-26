import { clone } from "@bufbuild/protobuf";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { PageAction, PageActionMenu, PageBreadcrumb } from "@kentos/components/layout/page";
import { cn } from "@kentos/components/lib/utils";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { EllipsisIcon, LayoutDashboardIcon, PlusIcon, Share2Icon, Trash2Icon, UsersIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { gutter, Page, useDocumentTitle } from "@/components/app/page";
import { EmptyState, ErrorState } from "@/components/common/states";
import { AddWidgetDialog, Widget } from "@/components/dashboard/widgets";
import {
  type Dashboard,
  DashboardSchema,
  type DashboardWidget,
  ViewService,
} from "@/gen/izci/api/v1/view_pb";
import { errorMessage, isNotFound } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canWrite } from "@/lib/session";

export const Route = createFileRoute("/o/$org/dashboards/$id")({ component: DashboardPage });

function DashboardPage() {
  const org = useOrg();
  const { id } = Route.useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const q = useQuery(ViewService.method.getDashboard, { id });
  const d = q.data?.dashboard;
  useDocumentTitle(d?.name);
  const [adding, setAdding] = useState(false);
  const invalidate = () =>
    qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: ViewService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
  const save = useMutation(ViewService.method.saveDashboard, {
    onSuccess: () => void invalidate(),
    onError: (err) => toast.error(errorMessage(err)),
  });
  const remove = useMutation(ViewService.method.deleteDashboard, {
    onSuccess: () => {
      toast.success("Pano silindi");
      void invalidate();
      void navigate({ to: "/o/$org/dashboards", params: { org: org.slug } });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });

  if (q.isPending) return <Skeleton className={cn("my-6 h-64", gutter)} />;
  if (q.error || !d) {
    return isNotFound(q.error) ? (
      <EmptyState title="Pano bulunamadı" className="py-24">
        Pano silinmiş ya da size kapalı olabilir.
      </EmptyState>
    ) : (
      <ErrorState error={q.error} onRetry={() => void q.refetch()} className="py-24" />
    );
  }
  const update = (change: (next: Dashboard) => void) => {
    const next = clone(DashboardSchema, d);
    change(next);
    save.mutate({ dashboard: next });
  };
  const move = (i: number, by: -1 | 1) =>
    update((next) => {
      const j = i + by;
      if (j < 0 || j >= next.widgets.length) return;
      const w = next.widgets;
      [w[i], w[j]] = [w[j] as DashboardWidget, w[i] as DashboardWidget];
    });

  return (
    <Page
      icon={LayoutDashboardIcon}
      title={d.name}
      breadcrumb={
        <PageBreadcrumb>
          <Link to="/o/$org/dashboards" params={{ org: org.slug }}>
            Panolar
          </Link>
          <span>{d.name}</span>
        </PageBreadcrumb>
      }
      description={
        d.shared ? (
          <span className="inline-flex items-center gap-1.5">
            <UsersIcon className="size-3.5" />
            Ortak pano, {d.owner?.name || d.owner?.email}
          </span>
        ) : (
          "Kişisel pano"
        )
      }
      actions={
        d.editable ? (
          <>
            {canWrite(org.role) ? (
              <PageAction
                icon={Share2Icon}
                label={d.shared ? "Paylaşmayı bırak" : "Paylaş"}
                description={d.shared ? "Pano yalnızca sizin olur" : "Organizasyondaki herkes görebilir"}
                disabled={save.isPending}
                onClick={() => update((next) => (next.shared = !next.shared))}
              />
            ) : null}
            <PageActionMenu icon={EllipsisIcon} label="Diğer işlemler" hideLabel>
              <PageAction
                icon={Trash2Icon}
                label="Panoyu sil"
                variant="destructive"
                disabled={remove.isPending}
                onClick={() => {
                  if (window.confirm(`${d.name} panosu silinsin mi?`)) remove.mutate({ id: d.id });
                }}
              />
            </PageActionMenu>
            <PageAction
              icon={PlusIcon}
              label="Bileşen ekle"
              description="Grafik ya da hata listesi"
              variant="default"
              disabled={d.widgets.length >= 24}
              onClick={() => setAdding(true)}
            />
          </>
        ) : null
      }
    >
      <div className={gutter}>
        {d.widgets.length === 0 ? (
          <EmptyState title="Bu panoda bileşen yok" className="card py-14">
            {d.editable
              ? "Bileşen ekle ile bir aramayı grafik ya da hata listesi olarak ekleyin."
              : "Panonun sahibi henüz bileşen eklemedi."}
          </EmptyState>
        ) : (
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {d.widgets.map((w, i) => (
              <Widget
                // biome-ignore lint/suspicious/noArrayIndexKey: widgets have no ids; their order is their identity.
                key={i}
                widget={w}
                editable={d.editable}
                onMove={(by) => move(i, by)}
                onRemove={() => update((next) => next.widgets.splice(i, 1))}
              />
            ))}
          </div>
        )}
      </div>
      <AddWidgetDialog
        open={adding}
        onOpenChange={setAdding}
        onAdd={(w) => update((next) => next.widgets.push(w))}
      />
    </Page>
  );
}
