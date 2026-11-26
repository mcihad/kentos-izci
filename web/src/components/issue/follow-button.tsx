import { createConnectQueryKey, useMutation } from "@connectrpc/connect-query";
import { Button } from "@kentos/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@kentos/components/ui/tooltip";
import { useQueryClient } from "@tanstack/react-query";
import { BellIcon, BellRingIcon } from "lucide-react";
import { toast } from "sonner";
import { type Issue, IssueService } from "@/gen/izci/api/v1/issue_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";

/** Follow an issue: hear when it comes back and when someone comments. */
export function FollowButton({ issue }: { issue: Issue }) {
  const org = useOrg();
  const qc = useQueryClient();
  const follow = useMutation(IssueService.method.followIssue, {
    onSuccess: (res) => {
      toast.success(res.following ? "Hatayı takip ediyorsunuz" : "Hatanın takibini bıraktınız");
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: IssueService.method.getIssue,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const on = issue.following;
  const id = issue.summary?.id ?? "";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          size="sm"
          variant="outline"
          aria-pressed={on}
          disabled={follow.isPending || !id}
          onClick={() => follow.mutate({ issueId: id, follow: !on })}
        >
          {on ? <BellRingIcon className="size-4" /> : <BellIcon className="size-4" />}
          {on ? "Takip ediliyor" : "Takip et"}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-64">
        Takip ettiğiniz hata geri dönünce ve yorum yazılınca bildirim alırsınız.
      </TooltipContent>
    </Tooltip>
  );
}
