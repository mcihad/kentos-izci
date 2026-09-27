import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { GitCommitHorizontalIcon, RefreshCwIcon } from "lucide-react";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { UserAvatar } from "@/components/common/user-avatar";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { Issue } from "@/gen/izci/api/v1/issue_pb";
import { OwnershipService, type SuspectCommit, SuspectCommitState } from "@/gen/izci/api/v1/ownership_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { useTriage } from "@/lib/triage";

/**
 * Who owns an issue by the project's ownership rules, and the suspect
 * commit: the commit that last changed the failing lines.
 */
export function IssueOwners({ issue, writable }: { issue: Issue; writable: boolean }) {
  const org = useOrg();
  const qc = useQueryClient();
  const triage = useTriage();
  const issueId = issue.summary?.id ?? "";
  const project = issue.summary?.project?.slug ?? "";
  const assignee = issue.summary?.assignee?.id ?? "";
  const q = useQuery(OwnershipService.method.getIssueOwners, { issueId }, { staleTime: 60_000 });
  const find = useMutation(OwnershipService.method.findSuspectCommit, {
    onSuccess: () =>
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: OwnershipService.method.getIssueOwners,
          input: { issueId },
          transport: org.transport,
          cardinality: "finite",
        }),
      }),
    onError: (err) => toast.error(errorMessage(err)),
  });
  // An issue nobody looked for yet is searched once, when first shown.
  const state = q.data?.suspectCommit?.state;
  const searched = useRef(false);
  useEffect(() => {
    if (state === SuspectCommitState.UNKNOWN && !searched.current) {
      searched.current = true;
      find.mutate({ issueId });
    }
  }, [state, issueId, find]);

  if (q.isPending) return <Skeleton className="h-24 w-full" />;
  if (!q.data) return null;
  const { owners, unmappedOwners } = q.data;
  const suspect = q.data.suspectCommit;
  const rulesLink = (
    <Link
      to="/o/$org/projects/$slug"
      params={{ org: org.slug, slug: project }}
      search={{ tab: "ownership" }}
      className="font-medium text-foreground underline-offset-4 hover:underline"
    >
      sahiplik kuralları
    </Link>
  );

  return (
    <div className="grid gap-5">
      <div>
        <h3 className="mb-2.5 font-semibold text-md">Sahipler</h3>
        {owners.length === 0 ? (
          <p className="text-sm text-muted-foreground leading-relaxed">
            Projenin {rulesLink} bu hata için kimseyi göstermiyor.
          </p>
        ) : (
          <ul className="grid gap-2.5">
            {owners.map((o) => (
              <li key={o.user?.id} className="flex items-start gap-2.5">
                <UserAvatar
                  name={o.user?.name || o.user?.email || ""}
                  src={o.user?.avatarUrl}
                  className="mt-0.5 size-6"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-md">
                    {o.user?.name || o.user?.email}
                    {o.suggested ? (
                      <span className="ml-1.5 text-muted-foreground text-sm">önerilen</span>
                    ) : null}
                  </p>
                  <p className="truncate font-mono text-muted-foreground text-xs" title={o.rules.join("\n")}>
                    {o.rules[0]}
                    {o.rules.length > 1 ? ` +${o.rules.length - 1}` : ""}
                  </p>
                </div>
                {writable && o.user && o.user.id !== assignee ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="-my-0.5 h-7 px-2"
                    disabled={triage.pending}
                    onClick={() => void triage.assign([issueId], o.user?.id ?? "")}
                  >
                    Ata
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
        {unmappedOwners.length > 0 ? (
          <p className="mt-2 text-muted-foreground text-xs leading-relaxed">
            Üyeyle eşlenmemiş sahipler: <span className="font-mono">{unmappedOwners.join(", ")}</span>.
            Eşlemek için {rulesLink} sayfasına gidin.
          </p>
        ) : null}
      </div>

      <div>
        <div className="mb-2.5 flex items-center justify-between gap-2">
          <h3 className="font-semibold text-md">Şüpheli commit</h3>
          {suspect && suspect.state > SuspectCommitState.UNKNOWN ? (
            <Button
              variant="ghost"
              size="icon"
              className="size-7 text-muted-foreground"
              aria-label="Yeniden ara"
              title="Yeniden ara"
              disabled={find.isPending}
              onClick={() => find.mutate({ issueId })}
            >
              <RefreshCwIcon className={find.isPending ? "size-3.5 animate-spin" : "size-3.5"} />
            </Button>
          ) : null}
        </div>
        <Suspect
          suspect={suspect}
          searching={find.isPending || state === SuspectCommitState.UNKNOWN}
          project={project}
          canAssign={writable && !!suspect?.author && suspect.author.id !== assignee}
          onAssign={() => void triage.assign([issueId], suspect?.author?.id ?? "")}
        />
      </div>
    </div>
  );
}

function Suspect({
  suspect,
  searching,
  project,
  canAssign,
  onAssign,
}: {
  suspect: SuspectCommit | undefined;
  searching: boolean;
  project: string;
  canAssign: boolean;
  onAssign: () => void;
}) {
  const org = useOrg();
  const repoLink = (text: string) => (
    <Link
      to="/o/$org/projects/$slug"
      params={{ org: org.slug, slug: project }}
      search={{ tab: "repository" }}
      className="font-medium text-foreground underline-offset-4 hover:underline"
    >
      {text}
    </Link>
  );
  if (searching) return <Skeleton className="h-16 w-full" />;
  const muted = (children: React.ReactNode) => (
    <p className="text-sm text-muted-foreground leading-relaxed">{children}</p>
  );
  switch (suspect?.state) {
    case SuspectCommitState.FOUND: {
      const who = suspect.author?.name || suspect.author?.email || suspect.authorName || suspect.authorEmail;
      return (
        <div className="grid gap-1.5 rounded-lg border px-3 py-2.5">
          <a
            href={suspect.url}
            target="_blank"
            rel="noreferrer"
            className="flex min-w-0 items-start gap-2 text-md underline-offset-4 hover:underline"
          >
            <GitCommitHorizontalIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <span className="line-clamp-2">{suspect.message || suspect.sha.slice(0, 7)}</span>
          </a>
          <p className="text-muted-foreground text-sm">
            <span className="font-mono">{suspect.sha.slice(0, 7)}</span>, {who}
            {suspect.committedAt ? (
              <>
                , <RelativeTime value={suspect.committedAt} />
              </>
            ) : null}
          </p>
          <p
            className="truncate font-mono text-muted-foreground text-xs"
            title={`${suspect.path}:${suspect.line}`}
          >
            {suspect.path}:{suspect.line}
          </p>
          {canAssign ? (
            <div>
              <Button variant="outline" size="sm" className="mt-1 h-7" onClick={onAssign}>
                {suspect.author?.name || suspect.author?.email} kişisine ata
              </Button>
            </div>
          ) : null}
        </div>
      );
    }
    case SuspectCommitState.NO_FRAMES:
      return muted("Olayın yığın izinde satır numarası olan çerçeve yok.");
    case SuspectCommitState.NOT_IN_REPOSITORY:
      return muted(
        <>
          Yığın izindeki dosyalar depoda bulunamadı. Uygulamanın yolları depodan farklıysa{" "}
          {repoLink("yol eşlemesi")} ekleyin.
        </>,
      );
    case SuspectCommitState.FAILED:
      return (
        <div className="grid gap-1">
          {muted("GitHub'dan yanıt alınamadı. Bağlantının yetkilerini kontrol edip yeniden deneyin.")}
          {suspect.detail ? (
            <p className="font-mono text-muted-foreground text-xs [overflow-wrap:anywhere]">
              {suspect.detail}
            </p>
          ) : null}
        </div>
      );
    default:
      return muted(
        <>
          Hatalı satırları en son değiştiren commit'i bulmak için projeye GitHub bağlantısıyla bir{" "}
          {repoLink("depo bağlayın")}.
        </>,
      );
  }
}
