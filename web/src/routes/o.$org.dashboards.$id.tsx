import { clone } from "@bufbuild/protobuf";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ChevronRightIcon, PlusIcon, Trash2Icon, UsersIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { gutter, useDocumentTitle } from "@/components/app/page";
import { EmptyState, ErrorState } from "@/components/common/states";
import { AddWidgetDialog, Widget } from "@/components/dashboard/widgets";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  type Dashboard,
  DashboardSchema,
  type DashboardWidget,
  ViewService,
} from "@/gen/izci/api/v1/view_pb";
import { errorMessage, isNotFound } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canWrite } from "@/lib/session";
import { cn } from "@/lib/utils";

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
    <div className="pb-12">
      <header className={cn("flex flex-wrap items-start gap-3 pt-4 pb-5", gutter)}>
        <div className="min-w-0 flex-1">
          <nav className="flex items-center gap-1.5 text-sm text-muted-foreground" aria-label="Konum">
            <Link to="/o/$org/dashboards" params={{ org: org.slug }} className="hover:text-foreground">
              Panolar
            </Link>
            <ChevronRightIcon className="size-3.5" />
          </nav>
          <h1 className="mt-1 truncate font-semibold text-xl">{d.name}</h1>
          <p className="mt-1 flex items-center gap-1.5 text-muted-foreground text-sm">
            {d.shared ? (
              <>
                <UsersIcon className="size-3.5" />
                Ortak pano, {d.owner?.name || d.owner?.email}
              </>
            ) : (
              "Kişisel pano"
            )}
          </p>
        </div>
        {d.editable ? (
          <div className="flex gap-2">
            {canWrite(org.role) ? (
              <Button
                variant="outline"
                size="sm"
                disabled={save.isPending}
                onClick={() => update((next) => (next.shared = !next.shared))}
              >
                {d.shared ? "Paylaşmayı bırak" : "Paylaş"}
              </Button>
            ) : null}
            <Button
              variant="outline"
              size="sm"
              onClick={() => setAdding(true)}
              disabled={d.widgets.length >= 24}
            >
              <PlusIcon className="size-4" />
              Bileşen ekle
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive"
              disabled={remove.isPending}
              onClick={() => {
                if (window.confirm(`${d.name} panosu silinsin mi?`)) remove.mutate({ id: d.id });
              }}
            >
              <Trash2Icon className="size-4" />
              Sil
            </Button>
          </div>
        ) : null}
      </header>
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
    </div>
  );
}
