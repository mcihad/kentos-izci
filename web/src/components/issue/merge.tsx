import { createConnectQueryKey, useMutation } from "@connectrpc/connect-query";
import { Button } from "@kentos/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Label } from "@kentos/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@kentos/components/ui/radio-group";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { SplitIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { type Issue, IssueService, type IssueSummary } from "@/gen/izci/api/v1/issue_pb";
import { errorMessage } from "@/lib/errors";
import { formatCompact } from "@/lib/format";
import { useOrg } from "@/lib/org";

function useInvalidateIssues() {
  const org = useOrg();
  const qc = useQueryClient();
  return () =>
    qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: IssueService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
}

/** Issues merged into this one, each of which can be moved out again. */
export function MergedIssues({ issue, writable }: { issue: Issue; writable: boolean }) {
  const org = useOrg();
  const navigate = useNavigate();
  const invalidate = useInvalidateIssues();
  const unmerge = useMutation(IssueService.method.unmergeIssue, {
    onSuccess: (res) => {
      const short = res.issue?.shortId ?? "";
      toast.success(`${short} ayrıldı`, {
        description: "Olayları birkaç dakika içinde taşınır ve sayaçları yeniden hesaplanır.",
        action: res.issue
          ? {
              label: "Aç",
              onClick: () =>
                void navigate({ to: "/o/$org/issues/$issueId", params: { org: org.slug, issueId: short } }),
            }
          : undefined,
      });
      void invalidate();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <div>
      <h3 className="mb-2 font-semibold text-md">Birleştirilen hatalar</h3>
      <ul className="grid gap-2.5">
        {issue.merged.map((m) => (
          <li key={m.fingerprint} className="grid gap-0.5 text-sm">
            <div className="flex items-baseline justify-between gap-2">
              <span className="font-mono text-xs">{m.shortId}</span>
              {writable ? (
                <Button
                  variant="ghost"
                  size="xs"
                  disabled={unmerge.isPending}
                  onClick={() =>
                    unmerge.mutate({ issueId: issue.summary?.id ?? "", fingerprint: m.fingerprint })
                  }
                >
                  <SplitIcon className="size-3.5" />
                  Ayır
                </Button>
              ) : null}
            </div>
            <span className="truncate" title={m.title}>
              {m.title}
            </span>
            <span className="text-muted-foreground text-xs">
              {m.mergedBy?.name || m.mergedBy?.email || "Bir üye"}, <RelativeTime value={m.mergedAt} />
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Merges the selected issues into the one the member keeps. */
export function MergeDialog({
  issues,
  open,
  onOpenChange,
  onMerged,
}: {
  issues: IssueSummary[];
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onMerged: () => void;
}) {
  const invalidate = useInvalidateIssues();
  const busiest = issues.reduce<IssueSummary | undefined>(
    (best, i) => (!best || i.timesSeen > best.timesSeen ? i : best),
    undefined,
  );
  const [target, setTarget] = useState<string>();
  const chosen = target ?? busiest?.id ?? "";
  const projects = new Set(issues.map((i) => i.project?.id));
  const merge = useMutation(IssueService.method.mergeIssues, {
    onSuccess: (res) => {
      toast.success(`${res.merged} hata ${res.issue?.shortId} içine birleştirildi`, {
        description: "Olaylar birkaç dakika içinde taşınır.",
      });
      void invalidate();
      onOpenChange(false);
      onMerged();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{issues.length} hata birleştirilsin mi?</DialogTitle>
          <DialogDescription>
            Hangi hatanın kalacağını seçin. Diğerlerinin olayları, sayaçları, yorumları ve takipçileri ona
            geçer; aynı parmak izleriyle gelen yeni olaylar da ona düşer. Birleştirmeyi hata sayfasındaki{" "}
            <b className="font-medium text-foreground">Ayır</b> ile geri alabilirsiniz.
          </DialogDescription>
        </DialogHeader>
        {projects.size > 1 ? (
          <p className="rounded-md border border-destructive/35 px-3 py-2 text-sm">
            Yalnız aynı projenin hataları birleştirilebilir. Seçimi tek bir projeyle sınırlayın.
          </p>
        ) : (
          <RadioGroup value={chosen} onValueChange={setTarget} className="max-h-72 gap-1 overflow-y-auto">
            {issues.map((i) => (
              <Label
                key={i.id}
                htmlFor={`merge-${i.id}`}
                className="flex items-start gap-2.5 rounded-md px-2 py-2 font-normal hover:bg-(--surface-hover)"
              >
                <RadioGroupItem value={i.id} id={`merge-${i.id}`} className="mt-0.5" />
                <span className="grid min-w-0 flex-1 gap-0.5">
                  <span className="truncate text-sm">
                    <span className="font-mono text-muted-foreground text-xs">{i.shortId}</span> {i.title}
                  </span>
                  <span className="text-muted-foreground text-xs">
                    {formatCompact(i.timesSeen)} olay, {formatCompact(i.usersCount)} kullanıcı
                    {i.id === busiest?.id ? ", en çok olay" : ""}
                  </span>
                </span>
              </Label>
            ))}
          </RadioGroup>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Vazgeç
          </Button>
          <Button
            disabled={projects.size > 1 || !chosen || merge.isPending}
            onClick={() =>
              merge.mutate({
                targetIssueId: chosen,
                issueIds: issues.filter((i) => i.id !== chosen).map((i) => i.id),
              })
            }
          >
            Birleştir
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
