import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { BellRingIcon, MessageCircleIcon, PlusIcon, SendIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { describeRule } from "@/components/alert/describe";
import { LinkChatDialog } from "@/components/alert/link-chat";
import { RuleDialog } from "@/components/alert/rule-dialog";
import { gutter, PageHeader, Section } from "@/components/app/page";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { type AlertRule, AlertService, type Channel } from "@/gen/izci/api/v1/alert_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/o/$org/alerts")({
  component: AlertsPage,
});

const chatTypes: Record<string, string> = {
  private: "Özel sohbet",
  group: "Grup",
  supergroup: "Grup",
  channel: "Kanal",
};

function AlertsPage() {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const qc = useQueryClient();
  const status = useQuery(AlertService.method.getNotificationStatus, {});
  const channels = useQuery(AlertService.method.listChannels, {});
  const rules = useQuery(AlertService.method.listAlertRules, {});
  const [linking, setLinking] = useState(false);
  const [editing, setEditing] = useState<AlertRule | "new">();
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
      toast.success("Sohbetin bağlantısı kaldırıldı");
      void refresh();
    },
    onError: fail,
  });
  const test = useMutation(AlertService.method.sendTestNotification, {
    onSuccess: () => toast.success("Test bildirimi gönderildi. Birkaç saniye içinde sohbette görünür."),
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
  const configured = status.data?.telegramConfigured ?? true;

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
      <PageHeader title="Uyarılar" description="Hangi hataların hangi Telegram sohbetlerine bildirileceği" />
      <div className={cn("max-w-4xl space-y-10 pt-6", gutter)}>
        {!configured ? (
          <div className="rounded-lg border border-level-warning/40 px-4 py-3 text-sm">
            <p className="font-medium">Telegram botu yapılandırılmamış</p>
            <p className="mt-0.5 text-muted-foreground">
              Sunucu yöneticisi <code className="font-mono">TELEGRAM_BOT_TOKEN</code> değişkenini
              ayarladığında sohbet bağlayabilir ve bildirim alabilirsiniz.
            </p>
          </div>
        ) : null}

        <Section
          title="Telegram sohbetleri"
          description={
            status.data?.telegramBotUsername
              ? `Bildirimleri @${status.data.telegramBotUsername} gönderir.`
              : undefined
          }
          actions={
            admin ? (
              <Button size="sm" disabled={!configured} onClick={() => setLinking(true)}>
                <PlusIcon className="size-4" />
                Sohbet bağla
              </Button>
            ) : null
          }
        >
          {!channels.data ? (
            <Skeleton className="h-24 w-full" />
          ) : chs.length === 0 ? (
            <EmptyState icon={MessageCircleIcon} title="Bağlı sohbet yok" className="card py-10">
              Ekibinizin Telegram grubunu bağlayın; yeni ve geri dönen hatalar oraya gelsin.
            </EmptyState>
          ) : (
            <ul className="divide-y card">
              {chs.map((c) => (
                <li key={c.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-medium text-md">{c.name}</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {chatTypes[c.telegram?.type ?? ""] ?? "Sohbet"}
                      {c.telegram?.threadId ? ", bir konu" : ""},{" "}
                      {c.project ? `${c.project.name} projesi` : "tüm projeler"},{" "}
                      {c.lastDeliveryAt ? (
                        <>
                          son bildirim <RelativeTime value={c.lastDeliveryAt} />
                        </>
                      ) : (
                        "henüz bildirim gitmedi"
                      )}
                    </p>
                    {c.lastError ? (
                      <p className="mt-1 text-xs text-destructive">Son gönderim başarısız: {c.lastError}</p>
                    ) : null}
                  </div>
                  {admin ? (
                    <div className="flex items-center gap-2">
                      <div className="mr-2 flex items-center gap-2">
                        <Switch
                          id={`mute-${c.id}`}
                          checked={!c.muted}
                          onCheckedChange={(on) =>
                            updateChannel.mutate({ id: c.id, name: c.name, muted: !on })
                          }
                        />
                        <Label htmlFor={`mute-${c.id}`} className="font-normal text-sm text-muted-foreground">
                          Bildirimler açık
                        </Label>
                      </div>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={test.isPending}
                        onClick={() => test.mutate({ channelId: c.id })}
                      >
                        <SendIcon className="size-3.5" />
                        Test
                      </Button>
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
              ))}
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
                ? "Önce bir sohbet bağlayın; ilk sohbetle birlikte iki temel kural eklenir."
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
