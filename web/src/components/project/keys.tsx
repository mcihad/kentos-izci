import { useMutation, useQuery } from "@connectrpc/connect-query";
import { SecretValue } from "@kentos/components/common/copy-button";
import { ConfirmDialog } from "@kentos/components/common/dialogs";
import { Field, FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Checkbox } from "@kentos/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Input } from "@kentos/components/ui/input";
import { Label } from "@kentos/components/ui/label";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { Switch } from "@kentos/components/ui/switch";
import { Textarea } from "@kentos/components/ui/textarea";
import { KeyRoundIcon, PlusIcon, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import {
  type ApiKey,
  ApiKeyScope,
  type CreateApiKeyResponse,
  type Project,
  ProjectService,
} from "@/gen/izci/api/v1/project_pb";
import { errorField, errorMessage, failWith } from "@/lib/errors";
import { formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { lines, useInvalidateProjects } from "./use-project";

const scopeLabels: Record<ApiKeyScope, string> = {
  [ApiKeyScope.UNSPECIFIED]: "",
  [ApiKeyScope.INGEST]: "Olay gönderme",
  [ApiKeyScope.RELEASES]: "Sürüm ve dosya yükleme",
};

export function ProjectKeys({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const invalidate = useInvalidateProjects();
  const keys = useQuery(ProjectService.method.listApiKeys, { project: project.slug });
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<ApiKey>();
  const update = useMutation(ProjectService.method.updateApiKey, {
    onSuccess: (res) => {
      toast.success(res.key?.enabled ? "Anahtar etkinleştirildi" : "Anahtar devre dışı bırakıldı");
      void invalidate();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const revoke = useMutation(ProjectService.method.revokeApiKey, {
    onSuccess: () => {
      toast.success("Anahtar iptal edildi");
      setRevoking(undefined);
      void invalidate();
    },
  });

  return (
    <div className="max-w-4xl py-6">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="font-semibold text-lg">API anahtarları</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Uygulamalar olayları bu anahtarlarla gönderir. Her ortam ya da uygulama için ayrı anahtar
            kullanmak iptali kolaylaştırır.
          </p>
        </div>
        {admin ? (
          <Button size="sm" onClick={() => setCreating(true)}>
            <PlusIcon className="size-4" />
            Anahtar oluştur
          </Button>
        ) : null}
      </div>
      {!keys.data ? (
        <Skeleton className="h-40 w-full" />
      ) : keys.data.keys.length === 0 ? (
        <EmptyState icon={KeyRoundIcon} title="Etkin anahtar yok" className="rounded-lg border">
          Olay göndermek için bir anahtar oluşturun.
        </EmptyState>
      ) : (
        <ul className="divide-y rounded-lg border">
          {keys.data.keys.map((k) => (
            <li key={k.id} className="flex flex-wrap items-start gap-x-4 gap-y-2 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="font-medium text-md">{k.name}</p>
                <p className="mt-0.5 font-mono text-xs text-muted-foreground">{k.prefix}…</p>
                <p className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>{k.scopes.map((s) => scopeLabels[s]).join(", ")}</span>
                  {k.allowedOrigins.length ? <span>Kaynaklar: {k.allowedOrigins.join(", ")}</span> : null}
                  {k.rateLimitPerMinute ? (
                    <span>Dakikada en çok {formatNumber(k.rateLimitPerMinute)} olay</span>
                  ) : null}
                  <span>
                    {k.lastUsedAt ? (
                      <>
                        Son kullanım <RelativeTime value={k.lastUsedAt} />
                      </>
                    ) : (
                      "Hiç kullanılmadı"
                    )}
                  </span>
                  <span>
                    <RelativeTime value={k.createdAt} /> oluşturuldu
                    {k.createdBy ? `, ${k.createdBy.name || k.createdBy.email}` : ""}
                  </span>
                </p>
                {k.scopes.includes(ApiKeyScope.RELEASES) ? (
                  k.browserSeenAt ? (
                    <p className="mt-2 flex gap-2 rounded-md border border-(--status-warning-border) bg-(--status-warning-bg) px-3 py-2 text-(--status-warning-fg) text-sm">
                      <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
                      <span>
                        Bu anahtar <RelativeTime value={k.browserSeenAt} /> bir tarayıcıdan kullanıldı;
                        herkese açık sayıldığı için sürüm kaydı ve dosya yükleme izni kapatıldı. Olay
                        göndermeye devam eder. CI için yükleme izinli ayrı bir anahtar oluşturun.
                      </span>
                    </p>
                  ) : (
                    <p className="mt-1.5 text-muted-foreground text-xs">
                      Yükleme izinli: yalnız CI'da kullanın. Bir tarayıcıdan olay gönderilirse yükleme izni
                      kendiliğinden kapanır.
                    </p>
                  )
                ) : null}
              </div>
              {admin ? (
                <div className="flex items-center gap-3">
                  <div className="flex items-center gap-2">
                    <Switch
                      id={`key-${k.id}`}
                      checked={k.enabled}
                      disabled={update.isPending}
                      onCheckedChange={(on) =>
                        update.mutate({
                          project: project.slug,
                          keyId: k.id,
                          enabled: on,
                          allowedOrigins: k.allowedOrigins,
                          rateLimitPerMinute: k.rateLimitPerMinute,
                        })
                      }
                    />
                    <Label htmlFor={`key-${k.id}`} className="font-normal text-sm text-muted-foreground">
                      Etkin
                    </Label>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    onClick={() => setRevoking(k)}
                  >
                    İptal et
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      <CreateKeyDialog project={project} open={creating} onOpenChange={setCreating} />
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(v) => !v && setRevoking(undefined)}
        tone="danger"
        icon={KeyRoundIcon}
        title={`${revoking?.name ?? "Anahtar"} iptal edilsin mi?`}
        description="Bu anahtarla gönderilen olaylar hemen reddedilmeye başlar. Bu işlem geri alınamaz."
        confirmLabel="Anahtarı iptal et"
        pendingLabel="İptal ediliyor…"
        onConfirm={() =>
          revoking
            ? revoke.mutateAsync({ project: project.slug, keyId: revoking.id }).catch(failWith)
            : undefined
        }
      />
    </div>
  );
}

function CreateKeyDialog({
  project,
  open,
  onOpenChange,
}: {
  project: Project;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const invalidate = useInvalidateProjects();
  const [name, setName] = useState("");
  const [releases, setReleases] = useState(false);
  const [origins, setOrigins] = useState("");
  const [rate, setRate] = useState("");
  const [created, setCreated] = useState<CreateApiKeyResponse>();
  const create = useMutation(ProjectService.method.createApiKey, {
    onSuccess: (res) => {
      setCreated(res);
      void invalidate();
    },
  });
  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v) {
      setCreated(undefined);
      setName("");
      setOrigins("");
      setRate("");
      setReleases(false);
      create.reset();
    }
  };
  const fieldError = (f: string) =>
    create.error && errorField(create.error).startsWith(f) ? errorMessage(create.error) : undefined;
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-lg">
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Anahtar oluşturuldu</DialogTitle>
              <DialogDescription>
                Anahtar yalnızca şimdi gösterilir. Kopyalayıp güvenli bir yere kaydedin.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-3">
              <Field label="Anahtar">
                <SecretValue value={created.secret} />
              </Field>
              <Field label="Sentry DSN" hint="Sentry SDK'ları için bu adresi kullanın.">
                <SecretValue value={created.sentryDsn} />
              </Field>
            </div>
            <DialogFooter>
              <Button onClick={() => close(false)}>Kaydettim, kapat</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Yeni API anahtarı</DialogTitle>
              <DialogDescription>{project.name} projesine olay göndermek için.</DialogDescription>
            </DialogHeader>
            <form
              className="grid gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                create.mutate({
                  project: project.slug,
                  name,
                  scopes: releases ? [ApiKeyScope.INGEST, ApiKeyScope.RELEASES] : [ApiKeyScope.INGEST],
                  allowedOrigins: lines(origins),
                  rateLimitPerMinute: Number(rate) || 0,
                });
              }}
            >
              <Field label="Ad" htmlFor="key-name" hint="Örneğin: üretim sunucuları, Android uygulaması">
                <Input
                  id="key-name"
                  required
                  autoFocus
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </Field>
              <div className="flex items-center gap-2">
                <Checkbox
                  id="key-releases"
                  checked={releases}
                  onCheckedChange={(v) => setReleases(v === true)}
                />
                <Label htmlFor="key-releases" className="font-normal text-sm">
                  CI hattından sürüm kaydına, kaynak eşlemesi ve sembol yüklemeye de izin ver
                </Label>
              </div>
              {releases ? (
                <p className="-mt-2 text-sm text-muted-foreground">
                  Bu anahtarı yalnız CI'da kullanın. Tarayıcıya ya da mobil uygulamaya konan anahtar herkese
                  açıktır; ona bu izni vermeyin. Anahtar bir tarayıcıdan olay gönderirse yükleme izni
                  kendiliğinden kapanır.
                </p>
              ) : null}
              <Field
                label="İzin verilen tarayıcı kaynakları"
                htmlFor="key-origins"
                error={fieldError("allowed_origins")}
                hint="Her satıra bir adres yazın; yalnız bu sitelerdeki tarayıcılar gönderebilir. Boş bırakırsanız her siteden kabul edilir (Sentry ile aynı). Sunucu uygulamaları bu listeden etkilenmez."
              >
                <Textarea
                  id="key-origins"
                  value={origins}
                  onChange={(e) => setOrigins(e.target.value)}
                  placeholder="https://kalem.example.com"
                  className="min-h-16 font-mono text-sm"
                />
              </Field>
              <Field
                label="Dakikalık olay sınırı"
                htmlFor="key-rate"
                hint="Boş ya da 0: proje sınırı geçerli olur."
              >
                <Input
                  id="key-rate"
                  type="number"
                  min={0}
                  value={rate}
                  onChange={(e) => setRate(e.target.value)}
                  className="w-40"
                />
              </Field>
              <FormError
                message={create.error && !errorField(create.error) ? errorMessage(create.error) : undefined}
              />
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => close(false)}>
                  Vazgeç
                </Button>
                <Button type="submit" disabled={create.isPending}>
                  Anahtarı oluştur
                </Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
