import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { BotIcon, ShieldOffIcon, SparklesIcon } from "lucide-react";
import { useState } from "react";
import { EmptyState, ErrorState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { type AiRun, AiService } from "@/gen/izci/api/v1/ai_pb";
import type { IssueSummary } from "@/gen/izci/api/v1/issue_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { formatDateTime } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { isActive, runStatus } from "./labels";
import { RunView } from "./run-view";
import { StartRunDialog } from "./start-dialog";
import { useInvalidateRuns } from "./use-run-watch";

/** The AI tab of an issue: its analyses and a way to start one. */
export function IssueAi({
  issue,
  eventId,
  runId,
  onSelectRun,
  writable,
}: {
  issue: IssueSummary;
  eventId?: string;
  runId?: string;
  onSelectRun: (id: string | undefined) => void;
  writable: boolean;
}) {
  const invalidate = useInvalidateRuns();
  const org = useOrg();
  const [starting, setStarting] = useState(false);
  const status = useQuery(AiService.method.getAiStatus, {});
  const runs = useQuery(AiService.method.listRuns, { issueId: issue.id, pageSize: 20 });
  const project = useQuery(
    ProjectService.method.getProject,
    { slug: issue.project?.slug ?? "" },
    { enabled: !!issue.project },
  );
  const repository = project.data?.project?.repository;

  if (runs.isPending || status.isPending) {
    return (
      <div className="space-y-3 py-6">
        <Skeleton className="h-5 w-1/3" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }
  if (runs.error) return <ErrorState error={runs.error} onRetry={() => void runs.refetch()} />;

  const list = runs.data.runs;
  const selected: AiRun | undefined = list.find((r) => r.id === runId) ?? list[0];
  const server = status.data?.enabled ?? false;
  // The organization decides whether its data goes to the provider.
  const enabled = server && (status.data?.organizationEnabled ?? false);
  const busy = list.some(isActive);
  const dialog = (
    <StartRunDialog
      key={String(!!repository)}
      issueId={issue.id}
      eventId={eventId}
      hasRepository={!!repository}
      open={starting}
      onOpenChange={setStarting}
      onStarted={(run) => {
        void invalidate();
        onSelectRun(run.id);
      }}
    />
  );

  if (list.length === 0) {
    return (
      <>
        {enabled ? (
          <EmptyState
            icon={SparklesIcon}
            title="Bu hata henüz analiz edilmedi"
            action={
              writable ? (
                <Button onClick={() => setStarting(true)}>
                  <SparklesIcon className="size-4" />
                  Analizi başlat
                </Button>
              ) : null
            }
          >
            Yapay zekâ yığın izini, olayın bağlamını {repository ? "ve bağlı depodaki kodu " : ""}inceleyip
            kök nedeni açıklar{repository ? ", bir düzeltme önerir" : ""}.
          </EmptyState>
        ) : server ? (
          <EmptyState
            icon={ShieldOffIcon}
            title="Yapay zekâ kullanımı bu organizasyonda kapalı"
            action={
              canAdmin(org.role) ? (
                <Button asChild variant="outline">
                  <Link to="/o/$org/settings" params={{ org: org.slug }} search={{ tab: "general" }}>
                    Organizasyon ayarlarını aç
                  </Link>
                </Button>
              ) : null
            }
          >
            Hata kayıtları ve kod, organizasyon izin verene kadar yapay zekâ sağlayıcısına gönderilmez.
            {canAdmin(org.role) ? "" : " Bir yöneticiden açmasını isteyin."}
          </EmptyState>
        ) : (
          <EmptyState icon={BotIcon} title="Yapay zekâ analizi kapalı">
            Sunucu yöneticisi <code className="font-mono text-xs">AI_ENABLED=true</code> ile açabilir;
            kurulumu <code className="font-mono text-xs">izci ai doctor</code> komutu kontrol eder.
          </EmptyState>
        )}
        {dialog}
      </>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-5 py-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {list.length > 1 ? (
          <Select value={selected?.id} onValueChange={(v) => onSelectRun(v)}>
            <SelectTrigger size="sm" className="w-auto min-w-64 text-sm" aria-label="Analiz">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {list.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {formatDateTime(r.createdAt)} · {runStatus[r.status].label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <p className="font-medium text-md">Yapay zekâ analizi</p>
        )}
        {writable && enabled ? (
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setStarting(true)}>
            <SparklesIcon className="size-4" />
            Yeniden analiz et
          </Button>
        ) : null}
      </div>
      {selected ? (
        <RunView key={selected.id} run={selected} repository={repository} writable={writable} />
      ) : null}
      {dialog}
    </div>
  );
}
