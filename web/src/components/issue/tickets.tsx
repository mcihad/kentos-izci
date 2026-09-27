import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { ExternalLinkIcon, TicketIcon } from "lucide-react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { AlertService, ChannelKind } from "@/gen/izci/api/v1/alert_pb";
import { type Issue, IssueService } from "@/gen/izci/api/v1/issue_pb";
import { errorMessage, integrationMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";

/** Tickets of an issue in Jira, and a button to open one. */
export function IssueTickets({ issue, writable }: { issue: Issue; writable: boolean }) {
  const org = useOrg();
  const qc = useQueryClient();
  const channels = useQuery(AlertService.method.listChannels, {}, { enabled: writable, staleTime: 60_000 });
  const jira = (channels.data?.channels ?? []).filter((c) => c.kind === ChannelKind.JIRA && !c.muted);
  const create = useMutation(IssueService.method.createTicket, {
    onSuccess: (res) => {
      const key = res.ticket?.key ?? "";
      toast.success(res.created ? `Jira'da ${key} açıldı` : `Bu hata için zaten ${key} var`, {
        action: res.ticket?.url
          ? { label: "Aç", onClick: () => window.open(res.ticket?.url, "_blank", "noopener") }
          : undefined,
      });
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: IssueService,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
    },
    onError: (err) => toast.error(integrationMessage(err) ?? errorMessage(err)),
  });
  const tickets = issue.tickets;
  const issueId = issue.summary?.id ?? "";
  if (tickets.length === 0 && (!writable || jira.length === 0)) return null;
  const open = (channelId: string) => create.mutate({ issue: issueId, channelId });
  const available = jira.filter((c) => !tickets.some((t) => t.channelId === c.id));

  return (
    <div>
      <h3 className="mb-2.5 font-semibold text-md">İş kayıtları</h3>
      {tickets.length > 0 ? (
        <ul className="mb-3 grid gap-2">
          {tickets.map((t) => (
            <li key={`${t.channelId}-${t.key}`} className="flex items-center justify-between gap-3 text-md">
              <a
                href={t.url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex min-w-0 items-center gap-1.5 font-mono underline-offset-4 hover:underline"
              >
                <TicketIcon className="size-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{t.key}</span>
                <ExternalLinkIcon className="size-3 shrink-0 text-muted-foreground" />
              </a>
              <span className="shrink-0 text-sm text-muted-foreground">
                <RelativeTime value={t.createdAt} />
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {writable && available.length === 1 ? (
        <Button
          variant="outline"
          size="sm"
          className="w-full"
          disabled={create.isPending}
          onClick={() => open(available[0].id)}
        >
          <TicketIcon className="size-4" />
          {create.isPending ? "Jira'ya gönderiliyor…" : "Jira'da kayıt aç"}
        </Button>
      ) : writable && available.length > 1 ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" className="w-full" disabled={create.isPending}>
              <TicketIcon className="size-4" />
              {create.isPending ? "Jira'ya gönderiliyor…" : "Jira'da kayıt aç"}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {available.map((c) => (
              <DropdownMenuItem key={c.id} onSelect={() => open(c.id)}>
                {c.name}
                <span className="ml-auto pl-3 font-mono text-xs text-muted-foreground">
                  {c.jira?.projectKey}
                </span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </div>
  );
}
