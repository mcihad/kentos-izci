import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@kentos/components/ui/dropdown-menu";
import { Label } from "@kentos/components/ui/label";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { Switch } from "@kentos/components/ui/switch";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
  BellRingIcon,
  ChevronDownIcon,
  HashIcon,
  MailIcon,
  MessageCircleIcon,
  MessagesSquareIcon,
  PlusIcon,
  SendIcon,
  TicketIcon,
  WebhookIcon,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { ChannelDialog, type FormKind } from "@/components/alert/channel-dialog";
import { describeRule } from "@/components/alert/describe";
import { LinkChatDialog } from "@/components/alert/link-chat";
import { RuleDialog } from "@/components/alert/rule-dialog";
import { gutter, PageHeader, Section } from "@/components/app/page";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { type AlertRule, AlertService, type Channel, ChannelKind } from "@/gen/izci/api/v1/alert_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";

export const Route = createFileRoute("/o/$org/alerts")({
  component: AlertsPage,
});

const chatTypes: Record<string, string> = {
  private: "Özel sohbet",
  group: "Grup",
  supergroup: "Grup",
  channel: "Kanal",
};

const kindIcons: Record<ChannelKind, typeof MailIcon> = {
  [ChannelKind.UNSPECIFIED]: BellRingIcon,
  [ChannelKind.TELEGRAM]: MessageCircleIcon,
  [ChannelKind.EMAIL]: MailIcon,
  [ChannelKind.SLACK]: HashIcon,
  [ChannelKind.TEAMS]: MessagesSquareIcon,
  [ChannelKind.WEBHOOK]: WebhookIcon,
  [ChannelKind.JIRA]: TicketIcon,
};

/** What a channel is and where it delivers, in one line. */
function channelTarget(c: Channel): string {
  switch (c.kind) {
    case ChannelKind.TELEGRAM:
      return `Telegram, ${(chatTypes[c.telegram?.type ?? ""] ?? "sohbet").toLocaleLowerCase("tr-TR")}${c.telegram?.threadId ? ", bir konu" : ""}`;
    case ChannelKind.EMAIL: {
      const [first, ...rest] = c.emailRecipients;
      return `E-posta, ${first ?? ""}${rest.length ? ` ve ${rest.length} alıcı daha` : ""}`;
    }
    case ChannelKind.SLACK:
      return `Slack, ${c.urlHint}`;
    case ChannelKind.TEAMS:
      return `Microsoft Teams, ${c.urlHint}`;
    case ChannelKind.WEBHOOK:
      return `Webhook, ${c.urlHint}${c.signed ? ", imzalı" : ""}`;
    case ChannelKind.JIRA:
      return `Jira, ${c.jira?.projectKey} projesi (${c.jira?.issueType})`;
    default:
      return "Kanal";
  }
}

const addable: { kind: FormKind | ChannelKind.TELEGRAM; label: string }[] = [
  { kind: ChannelKind.TELEGRAM, label: "Telegram sohbeti" },
  { kind: ChannelKind.EMAIL, label: "E-posta" },
  { kind: ChannelKind.SLACK, label: "Slack" },
  { kind: ChannelKind.TEAMS, label: "Microsoft Teams" },
  { kind: ChannelKind.WEBHOOK, label: "Webhook" },
  { kind: ChannelKind.JIRA, label: "Jira" },
];

function AlertsPage() {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const qc = useQueryClient();
  const status = useQuery(AlertService.method.getNotificationStatus, {});
  const channels = useQuery(AlertService.method.listChannels, {});
  const rules = useQuery(AlertService.method.listAlertRules, {});
  const [linking, setLinking] = useState(false);
  const [editing, setEditing] = useState<AlertRule | "new">();
  const [channelForm, setChannelForm] = useState<{ kind: FormKind; channel?: Channel }>();
  const refresh = () =>
    qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: AlertService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
  const fail = (err: unknown) => toast.error(errorMessage(err));
  const updateChannel = useMutation(AlertService.method.updateChannel, {
    onSuccess: () => void refresh(),
    onError: fail,
  });
  const deleteChannel = useMutation(AlertService.method.deleteChannel, {
    onSuccess: () => {
      toast.success("Kanal kaldırıldı");
      void refresh();
    },
    onError: fail,
  });
  const test = useMutation(AlertService.method.sendTestNotification, {
    onSuccess: () => toast.success("Test bildirimi sıraya alındı. Birkaç saniye içinde kanala ulaşır."),
    onError: fail,
  });
  const updateRule = useMutation(AlertService.method.updateAlertRule, {
    onSuccess: () => void refresh(),
    onError: fail,
  });
  const deleteRule = useMutation(AlertService.method.deleteAlertRule, {
    onSuccess: () => {
      toast.success("Kural silindi");
      void refresh();
    },
    onError: fail,
  });
  const chs: Channel[] = channels.data?.channels ?? [];
  const telegram = status.data?.telegramConfigured ?? true;
  const email = status.data?.emailConfigured ?? true;
  const unavailable = (k: ChannelKind) =>
    k === ChannelKind.TELEGRAM ? !telegram : k === ChannelKind.EMAIL ? !email : false;

  const toggleRule = (r: AlertRule, enabled: boolean) =>
    updateRule.mutate({
      id: r.id,
      rule: {
        name: r.name,
        project: r.project?.slug ?? "",
        enabled,
        trigger: r.trigger,
        filters: r.filters,
        channelIds: r.channelIds,
        throttle: r.throttle,
      },
    });

  return (
    <div className="pb-12">
      <PageHeader title="Uyarılar" description="Hangi hataların hangi kanallara bildirileceği" />
      <div className={cn("max-w-4xl space-y-10 pt-6", gutter)}>
        <Section
          title="Kanallar"
          description={
            status.data?.telegramBotUsername
              ? `Telegram bildirimlerini @${status.data.telegramBotUsername} gönderir.`
              : undefined
          }
          actions={
            admin ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="sm">
                    <PlusIcon className="size-4" />
                    Kanal ekle
                    <ChevronDownIcon className="size-3.5 opacity-70" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-64">
                  {addable.map((a) => {
                    const Icon = kindIcons[a.kind];
                    const off = unavailable(a.kind);
                    return (
                      <DropdownMenuItem
                        key={a.kind}
                        disabled={off}
                        onSelect={() =>
                          a.kind === ChannelKind.TELEGRAM
                            ? setLinking(true)
                            : setChannelForm({ kind: a.kind })
                        }
                        className="items-start"
                      >
                        <Icon className="mt-0.5 size-4" />
                        <span className="grid">
                          {a.label}
                          {off ? (
                            <span className="text-xs text-muted-foreground">Sunucuda yapılandırılmamış</span>
                          ) : null}
                        </span>
                      </DropdownMenuItem>
                    );
                  })}
                </DropdownMenuContent>
              </DropdownMenu>
            ) : null
          }
        >
          {!channels.data ? (
            <Skeleton className="h-24 w-full" />
          ) : chs.length === 0 ? (
            <EmptyState icon={BellRingIcon} title="Kanal yok" className="card py-10">
              E-posta, Slack, Teams, Telegram, webhook ya da Jira ekleyin; yeni ve geri dönen hatalar oraya
              gelsin.
            </EmptyState>
          ) : (
            <ul className="divide-y card max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
              {chs.map((c) => {
                const Icon = kindIcons[c.kind] ?? BellRingIcon;
                return (
                  <li key={c.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
                    <Icon className="size-5 shrink-0 text-muted-foreground" aria-hidden />
                    <div className="min-w-0 flex-1">
                      <p className="truncate font-medium text-md">{c.name}</p>
                      <p className="mt-0.5 break-words text-xs text-muted-foreground">
                        {channelTarget(c)}, {c.project ? `${c.project.name} projesi` : "tüm projeler"},{" "}
                        {c.lastDeliveryAt ? (
                          <>
                            son bildirim <RelativeTime value={c.lastDeliveryAt} />
                          </>
                        ) : (
                          "henüz bildirim gitmedi"
                        )}
                      </p>
                      {c.lastError ? (
                        <p className="mt-1 break-words text-xs text-destructive">
                          Son gönderim başarısız: {c.lastError}
                        </p>
                      ) : null}
                    </div>
                    {admin ? (
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="mr-2 flex items-center gap-2">
                          <Switch
                            id={`mute-${c.id}`}
                            checked={!c.muted}
                            onCheckedChange={(on) =>
                              updateChannel.mutate({ id: c.id, name: c.name, muted: !on })
                            }
                          />
                          <Label
                            htmlFor={`mute-${c.id}`}
                            className="font-normal text-sm text-muted-foreground"
                          >
                            Bildirimler açık
                          </Label>
                        </div>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={test.isPending || unavailable(c.kind)}
                          onClick={() => test.mutate({ channelId: c.id })}
                        >
                          <SendIcon className="size-3.5" />
                          Test
                        </Button>
                        {c.kind !== ChannelKind.TELEGRAM ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setChannelForm({ kind: c.kind as FormKind, channel: c })}
                          >
                            Düzenle
                          </Button>
                        ) : null}
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-destructive"
                          onClick={() => deleteChannel.mutate({ id: c.id })}
                        >
                          Kaldır
                        </Button>
                      </div>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          )}
        </Section>

        <Section
          title="Kurallar"
          actions={
            admin ? (
              <Button
                size="sm"
                variant="outline"
                disabled={chs.length === 0}
                onClick={() => setEditing("new")}
              >
                <PlusIcon className="size-4" />
                Kural ekle
              </Button>
            ) : null
          }
        >
          {!rules.data ? (
            <Skeleton className="h-24 w-full" />
          ) : rules.data.rules.length === 0 ? (
            <EmptyState icon={BellRingIcon} title="Kural yok" className="card py-10">
              {chs.length === 0
                ? "Önce bir kanal ekleyin; ilk kanalla birlikte iki temel kural eklenir."
                : "Bir kural ekleyerek başlayın."}
            </EmptyState>
          ) : (
            <ul className="divide-y card">
              {rules.data.rules.map((r) => (
                <li key={r.id} className="flex flex-wrap items-start gap-x-4 gap-y-2 px-4 py-3">
                  <Switch
                    checked={r.enabled}
                    disabled={!admin}
                    onCheckedChange={(on) => toggleRule(r, on)}
                    aria-label={`${r.name} kuralı açık`}
                    className="mt-0.5"
                  />
                  <div className="min-w-0 flex-1">
                    <p className={cn("font-medium text-md", !r.enabled && "text-muted-foreground")}>
                      {r.name}
                    </p>
                    <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
                      {describeRule(r, chs)}
                    </p>
                    {r.lastTriggeredAt ? (
                      <p className="mt-1 text-xs text-muted-foreground">
                        Son tetiklenme <RelativeTime value={r.lastTriggeredAt} />
                      </p>
                    ) : null}
                  </div>
                  {admin ? (
                    <div className="flex gap-1">
                      <Button variant="ghost" size="sm" onClick={() => setEditing(r)}>
                        Düzenle
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-destructive"
                        onClick={() => deleteRule.mutate({ id: r.id })}
                      >
                        Sil
                      </Button>
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </Section>
      </div>
      <LinkChatDialog open={linking} onOpenChange={setLinking} />
      {channelForm ? (
        <ChannelDialog
          key={channelForm.channel?.id ?? channelForm.kind}
          kind={channelForm.kind}
          channel={channelForm.channel}
          open
          onOpenChange={(v) => !v && setChannelForm(undefined)}
          onSaved={() => void refresh()}
        />
      ) : null}
      {editing ? (
        <RuleDialog
          key={editing === "new" ? "new" : editing.id}
          rule={editing === "new" ? undefined : editing}
          channels={chs}
          open
          onOpenChange={(v) => !v && setEditing(undefined)}
        />
      ) : null}
    </div>
  );
}
