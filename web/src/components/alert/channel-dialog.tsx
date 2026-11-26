import { create } from "@bufbuild/protobuf";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { SecretValue } from "@kentos/components/common/copy-button";
import { Field, FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Input } from "@kentos/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Textarea } from "@kentos/components/ui/textarea";
import { CheckCircle2Icon } from "lucide-react";
import { useState } from "react";
import {
  AlertService,
  type Channel,
  ChannelKind,
  type ChannelSettings,
  ChannelSettingsSchema,
} from "@/gen/izci/api/v1/alert_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorMessage, integrationMessage } from "@/lib/errors";

/** Channel kinds created with a form (Telegram chats are linked instead). */
export type FormKind =
  | ChannelKind.EMAIL
  | ChannelKind.SLACK
  | ChannelKind.TEAMS
  | ChannelKind.WEBHOOK
  | ChannelKind.JIRA;

const copy: Record<FormKind, { title: string; description: string; name: string }> = {
  [ChannelKind.EMAIL]: {
    title: "E-posta kanalı",
    description:
      "Bildirimler bu adreslere gider; aynı hatanın sonraki bildirimleri aynı e-posta konuşmasına eklenir.",
    name: "Ekip e-postası",
  },
  [ChannelKind.SLACK]: {
    title: "Slack kanalı",
    description:
      "Slack'te bir uygulama oluşturup Incoming Webhooks özelliğini açın, bildirimlerin gideceği kanalı seçin ve verilen adresi yapıştırın.",
    name: "Slack #hatalar",
  },
  [ChannelKind.TEAMS]: {
    title: "Microsoft Teams kanalı",
    description:
      'Teams kanalında Workflows ile "Webhook isteği alındığında kanala gönder" akışını oluşturup adresini yapıştırın.',
    name: "Teams Hatalar",
  },
  [ChannelKind.WEBHOOK]: {
    title: "Webhook",
    description:
      "İzci her bildirimde bu adrese imzalı bir JSON gönderir. İmzayı doğrulamak için gereken anahtar kaydettikten sonra bir kez gösterilir.",
    name: "Olay yönetimi",
  },
  [ChannelKind.JIRA]: {
    title: "Jira projesi",
    description:
      "Uyarı kuralları bu kanala bildirdiğinde hata için Jira'da iş kaydı açılır; sonraki bildirimler kayda yorum olarak eklenir. Hata sayfasından da elle kayıt açabilirsiniz.",
    name: "Jira",
  },
};

type Form = {
  name: string;
  project: string;
  recipients: string;
  url: string;
  baseUrl: string;
  projectKey: string;
  issueType: string;
  email: string;
  token: string;
  labels: string;
};

function initial(kind: FormKind, channel?: Channel): Form {
  return {
    name: channel?.name ?? copy[kind].name,
    project: channel?.project?.slug ?? "*",
    recipients: channel?.emailRecipients.join("\n") ?? "",
    url: "",
    baseUrl: channel?.jira?.baseUrl ?? "",
    projectKey: channel?.jira?.projectKey ?? "",
    issueType: channel?.jira?.issueType ?? "Bug",
    email: channel?.jira?.email ?? "",
    token: "",
    labels: channel?.jira?.labels.join(", ") ?? "",
  };
}

function settingsOf(kind: FormKind, f: Form): ChannelSettings {
  const url = { url: f.url.trim() };
  switch (kind) {
    case ChannelKind.EMAIL:
      return create(ChannelSettingsSchema, {
        kind: {
          case: "email",
          value: {
            recipients: f.recipients
              .split(/[\s,;]+/)
              .map((s) => s.trim())
              .filter(Boolean),
          },
        },
      });
    case ChannelKind.SLACK:
      return create(ChannelSettingsSchema, { kind: { case: "slack", value: url } });
    case ChannelKind.TEAMS:
      return create(ChannelSettingsSchema, { kind: { case: "teams", value: url } });
    case ChannelKind.WEBHOOK:
      return create(ChannelSettingsSchema, { kind: { case: "webhook", value: url } });
    case ChannelKind.JIRA:
      return create(ChannelSettingsSchema, {
        kind: {
          case: "jira",
          value: {
            baseUrl: f.baseUrl.trim(),
            projectKey: f.projectKey.trim(),
            issueType: f.issueType.trim(),
            email: f.email.trim(),
            token: f.token.trim(),
            labels: f.labels
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean),
          },
        },
      });
  }
}

/** A failed Jira check is explained with Jira's status and message. */
function failure(err: unknown): string {
  return integrationMessage(err) ?? errorMessage(err);
}

/** Creates an e-mail, Slack, Teams, webhook or Jira channel, or edits one. */
export function ChannelDialog({
  kind,
  channel,
  open,
  onOpenChange,
  onSaved,
}: {
  kind: FormKind;
  channel?: Channel;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const projects = useQuery(ProjectService.method.listProjects, {}, { enabled: open && !channel });
  const [form, setForm] = useState(() => initial(kind, channel));
  const [secret, setSecret] = useState("");
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const createMutation = useMutation(AlertService.method.createChannel, {
    onSuccess: (res) => {
      onSaved();
      if (res.webhookSecret) setSecret(res.webhookSecret);
      else onOpenChange(false);
    },
  });
  const updateMutation = useMutation(AlertService.method.updateChannel, {
    onSuccess: () => {
      onSaved();
      onOpenChange(false);
    },
  });
  const pending = createMutation.isPending || updateMutation.isPending;
  const error = createMutation.error ?? updateMutation.error;
  const c = copy[kind];
  const editing = !!channel;
  const webhookKind =
    kind === ChannelKind.SLACK || kind === ChannelKind.TEAMS || kind === ChannelKind.WEBHOOK;

  const submit = () => {
    const settings = settingsOf(kind, form);
    if (channel) {
      updateMutation.mutate({ id: channel.id, name: form.name.trim(), muted: channel.muted, settings });
    } else {
      createMutation.mutate({
        name: form.name.trim(),
        project: form.project === "*" ? "" : form.project,
        settings,
      });
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (v) setForm(initial(kind, channel));
        setSecret("");
        createMutation.reset();
        updateMutation.reset();
        onOpenChange(v);
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        {secret ? (
          <>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <CheckCircle2Icon className="size-5 text-status-success-fg" />
                Webhook eklendi
              </DialogTitle>
              <DialogDescription>
                Alıcı tarafta isteklerin İzci'den geldiğini doğrulamak için bu imza anahtarını saklayın. Bir
                daha gösterilmez.
              </DialogDescription>
            </DialogHeader>
            <SecretValue value={secret} />
            <p className="text-sm text-muted-foreground">
              Her istek <code className="font-mono">X-Izci-Timestamp</code> ve{" "}
              <code className="font-mono">X-Izci-Signature</code> başlıklarını taşır. Doğrulama adımları
              belgelerde anlatılıyor.
            </p>
            <DialogFooter>
              <Button onClick={() => onOpenChange(false)}>Tamam</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>{editing ? `${channel.name} kanalını düzenle` : c.title}</DialogTitle>
              <DialogDescription>{c.description}</DialogDescription>
            </DialogHeader>
            <form
              className="grid gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                submit();
              }}
            >
              <Field label="Ad" htmlFor="channel-name">
                <Input
                  id="channel-name"
                  required
                  maxLength={100}
                  value={form.name}
                  onChange={(e) => set("name", e.target.value)}
                />
              </Field>
              {editing ? null : (
                <Field
                  label="Proje"
                  htmlFor="channel-project"
                  hint="Bilgi amaçlıdır; kanala neyin gideceğini uyarı kuralları belirler."
                >
                  <Select value={form.project} onValueChange={(v) => set("project", v)}>
                    <SelectTrigger id="channel-project" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="*">Tüm projeler</SelectItem>
                      {(projects.data?.projects ?? []).map((p) => (
                        <SelectItem key={p.id} value={p.slug}>
                          {p.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              )}
              {kind === ChannelKind.EMAIL ? (
                <Field
                  label="Alıcılar"
                  htmlFor="channel-recipients"
                  hint="Her satıra bir adres; en çok 20 alıcı."
                >
                  <Textarea
                    id="channel-recipients"
                    required
                    rows={4}
                    placeholder={"ekip@kentos.com.tr\nnobetci@kentos.com.tr"}
                    value={form.recipients}
                    onChange={(e) => set("recipients", e.target.value)}
                  />
                </Field>
              ) : null}
              {webhookKind ? (
                <Field
                  label="Webhook adresi"
                  htmlFor="channel-url"
                  hint={
                    editing
                      ? `Şu an: ${channel.urlHint}. Değiştirmek istemiyorsanız boş bırakın.`
                      : kind === ChannelKind.WEBHOOK
                        ? "http ya da https; adres gizli saklanır."
                        : "https ile başlar; adres gizli saklanır."
                  }
                >
                  <Input
                    id="channel-url"
                    type="url"
                    required={!editing}
                    className="font-mono"
                    placeholder={
                      kind === ChannelKind.SLACK
                        ? "https://hooks.slack.com/services/…"
                        : kind === ChannelKind.TEAMS
                          ? "https://…environment.api.powerplatform.com/…"
                          : "https://olay.kentos.com.tr/izci"
                    }
                    value={form.url}
                    onChange={(e) => set("url", e.target.value)}
                  />
                </Field>
              ) : null}
              {kind === ChannelKind.JIRA ? (
                <>
                  <Field label="Jira adresi" htmlFor="jira-url">
                    <Input
                      id="jira-url"
                      type="url"
                      required
                      placeholder="https://kentos.atlassian.net"
                      value={form.baseUrl}
                      onChange={(e) => set("baseUrl", e.target.value)}
                    />
                  </Field>
                  <div className="grid grid-cols-2 gap-4">
                    <Field label="Proje anahtarı" htmlFor="jira-project">
                      <Input
                        id="jira-project"
                        required
                        className="font-mono uppercase"
                        placeholder="KALEM"
                        value={form.projectKey}
                        onChange={(e) => set("projectKey", e.target.value)}
                      />
                    </Field>
                    <Field label="Kayıt türü" htmlFor="jira-type" hint="Jira'daki adıyla.">
                      <Input
                        id="jira-type"
                        value={form.issueType}
                        onChange={(e) => set("issueType", e.target.value)}
                      />
                    </Field>
                  </div>
                  <Field
                    label="Hesap e-postası"
                    htmlFor="jira-email"
                    hint="Jira Cloud'da API anahtarının sahibi. Jira Server ve Data Center'da kişisel erişim anahtarı kullanıyorsanız boş bırakın."
                  >
                    <Input
                      id="jira-email"
                      type="email"
                      value={form.email}
                      onChange={(e) => set("email", e.target.value)}
                    />
                  </Field>
                  <Field
                    label="API anahtarı"
                    htmlFor="jira-token"
                    hint={
                      editing
                        ? "Değiştirmek istemiyorsanız boş bırakın."
                        : "Kaydederken Jira'ya bağlanıp projeyi kontrol ederiz; anahtar şifreli saklanır."
                    }
                  >
                    <Input
                      id="jira-token"
                      type="password"
                      autoComplete="off"
                      required={!editing}
                      value={form.token}
                      onChange={(e) => set("token", e.target.value)}
                    />
                  </Field>
                  <Field
                    label="Etiketler"
                    htmlFor="jira-labels"
                    hint="Virgülle ayırın; izci etiketi hep eklenir."
                  >
                    <Input
                      id="jira-labels"
                      placeholder="kalem, arka-uc"
                      value={form.labels}
                      onChange={(e) => set("labels", e.target.value)}
                    />
                  </Field>
                </>
              ) : null}
              <FormError message={error ? failure(error) : undefined} />
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                  Vazgeç
                </Button>
                <Button type="submit" disabled={pending || !form.name.trim()}>
                  {pending
                    ? kind === ChannelKind.JIRA
                      ? "Jira kontrol ediliyor…"
                      : "Kaydediliyor…"
                    : editing
                      ? "Değişiklikleri kaydet"
                      : "Kanalı ekle"}
                </Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
