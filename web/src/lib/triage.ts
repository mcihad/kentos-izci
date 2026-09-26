import type { MessageInitShape } from "@bufbuild/protobuf";
import { createConnectQueryKey, useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { IssueStatus, type Priority } from "@/gen/izci/api/v1/common_pb";
import { DashboardService } from "@/gen/izci/api/v1/dashboard_pb";
import { IssueService, type UpdateIssuesRequestSchema } from "@/gen/izci/api/v1/issue_pb";
import { errorMessage } from "./errors";
import { priorityLabels } from "./labels";
import { useOrg } from "./org";
import { useSession } from "./session";

type Update = MessageInitShape<typeof UpdateIssuesRequestSchema>;

function count(ids: string[], one: string, many: string) {
  return ids.length === 1 ? one : `${ids.length} ${many}`;
}

/** Triage actions on one or many issues, with feedback and cache refresh. */
export function useTriage() {
  const org = useOrg();
  const qc = useQueryClient();
  const me = useSession()?.user?.id ?? "";
  const mutation = useMutation(IssueService.method.updateIssues, {
    onError: (err) => toast.error(errorMessage(err)),
    onSettled: () => {
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: IssueService,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: DashboardService,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
    },
  });

  const run = (req: Update, done: string) =>
    mutation.mutateAsync(req).then(
      (res) => {
        toast.success(done);
        return res;
      },
      () => undefined,
    );

  return {
    pending: mutation.isPending,
    me,
    resolve: (ids: string[], opts?: { inRelease?: string; inNextRelease?: boolean }) =>
      run(
        { issueIds: ids, status: IssueStatus.RESOLVED, resolve: opts },
        count(ids, "Çözüldü olarak işaretlendi", "hata çözüldü olarak işaretlendi"),
      ),
    ignore: (ids: string[], opts?: Update["ignore"]) =>
      run(
        { issueIds: ids, status: IssueStatus.IGNORED, ignore: opts ?? {} },
        count(ids, "Yoksayıldı", "hata yoksayıldı"),
      ),
    reopen: (ids: string[]) =>
      run(
        { issueIds: ids, status: IssueStatus.UNRESOLVED },
        count(ids, "Yeniden açıldı", "hata yeniden açıldı"),
      ),
    assign: (ids: string[], userId: string) =>
      run(
        { issueIds: ids, assigneeId: userId },
        userId === ""
          ? "Atama kaldırıldı"
          : userId === me
            ? count(ids, "Size atandı", "hata size atandı")
            : "Atandı",
      ),
    setPriority: (ids: string[], priority: Priority) =>
      run(
        { issueIds: ids, priority },
        `Öncelik ${priorityLabels[priority].toLocaleLowerCase("tr-TR")} yapıldı`,
      ),
  };
}
