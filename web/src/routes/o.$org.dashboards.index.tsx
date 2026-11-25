import { useMutation, useQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { LayoutDashboardIcon, PlusIcon, UsersIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { gutter, PageHeader } from "@/components/app/page";
import { Field } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ViewService } from "@/gen/izci/api/v1/view_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canWrite } from "@/lib/session";

export const Route = createFileRoute("/o/$org/dashboards/")({ component: DashboardsPage });

function DashboardsPage() {
  const org = useOrg();
  const q = useQuery(ViewService.method.listDashboards, {});
  const [creating, setCreating] = useState(false);
  return (
    <div className="pb-12">
      <PageHeader
        title="Panolar"
        description="Kayıtlı aramaları grafik ve listelerle bir arada izleyin."
        actions={
          <Button size="sm" onClick={() => setCreating(true)}>
            <PlusIcon className="size-4" />
            Yeni pano
          </Button>
        }
      />
      <div className={gutter}>
        {q.isPending ? (
          <Skeleton className="h-40 w-full" />
        ) : q.error ? (
          <ErrorState error={q.error} onRetry={() => void q.refetch()} />
        ) : q.data.dashboards.length === 0 ? (
          <EmptyState title="Henüz pano yok" className="card py-14">
            Bir pano oluşturup ekibinizin izlediği aramaları (ör. bir belediyenin kritik hataları) grafik ya
            da hata listesi olarak ekleyin.
          </EmptyState>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {q.data.dashboards.map((d) => (
              <li key={d.id}>
                <Link
                  to="/o/$org/dashboards/$id"
                  params={{ org: org.slug, id: d.id }}
                  className="card flex h-full items-start gap-3 px-4 py-4 transition-colors duration-(--duration-fast) hover:bg-(--surface-hover)"
                >
                  <LayoutDashboardIcon className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
                  <span className="grid min-w-0 gap-0.5">
                    <span className="truncate font-medium">{d.name}</span>
                    <span className="flex items-center gap-1.5 text-muted-foreground text-xs">
                      {d.shared ? (
                        <>
                          <UsersIcon className="size-3.5" />
                          Ortak · {d.owner?.name || d.owner?.email}
                        </>
                      ) : (
                        "Kişisel"
                      )}
                      {" · "}
                      <RelativeTime value={d.updatedAt} />
                    </span>
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
      <NewDashboardDialog open={creating} onOpenChange={setCreating} canShare={canWrite(org.role)} />
    </div>
  );
}

function NewDashboardDialog({
  open,
  onOpenChange,
  canShare,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  canShare: boolean;
}) {
  const org = useOrg();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [shared, setShared] = useState(false);
  const save = useMutation(ViewService.method.saveDashboard, {
    onSuccess: (res) => {
      onOpenChange(false);
      void navigate({ to: "/o/$org/dashboards/$id", params: { org: org.slug, id: res.dashboard?.id ?? "" } });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Yeni pano</DialogTitle>
          <DialogDescription>Oluşturduktan sonra bileşen ekleyin.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate({ dashboard: { name, shared } });
          }}
        >
          <Field label="Ad" htmlFor="board-name">
            <Input
              id="board-name"
              required
              maxLength={100}
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
          </Field>
          {canShare ? (
            <div className="flex items-center gap-2.5">
              <Checkbox id="board-shared" checked={shared} onCheckedChange={(v) => setShared(v === true)} />
              <Label htmlFor="board-shared" className="font-normal text-sm">
                Organizasyonla paylaş
              </Label>
            </div>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={save.isPending || !name.trim()}>
              Oluştur
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
