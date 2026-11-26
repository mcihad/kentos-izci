import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { type AlertRule, AlertService, type Channel, TriggerKind } from "@/gen/izci/api/v1/alert_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { Level } from "@/gen/izci/ingest/v1/event_pb";
import { errorField, errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { seconds } from "./describe";

const windows = [
  { value: "300", label: "5 dakika" },
  { value: "900", label: "15 dakika" },
  { value: "3600", label: "1 saat" },
  { value: "21600", label: "6 saat" },
  { value: "86400", label: "24 saat" },
];
const throttles = [
  { value: "0", label: "Her seferinde" },
  { value: "300", label: "5 dakikada bir" },
  { value: "1800", label: "30 dakikada bir" },
  { value: "3600", label: "Saatte bir" },
  { value: "21600", label: "6 saatte bir" },
  { value: "86400", label: "Günde bir" },
];
const levelsFilter = [
  { value: String(Level.UNSPECIFIED), label: "Tüm seviyeler" },
  { value: String(Level.WARNING), label: "Uyarı ve üstü" },
  { value: String(Level.ERROR), label: "Hata ve üstü" },
  { value: String(Level.FATAL), label: "Yalnız ölümcül" },
];

const csv = (s: string) =>
  s
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);

/** Creates or edits an alert rule. */
export function RuleDialog({
  rule,
  channels,
  open,
  onOpenChange,
}: {
  rule?: AlertRule;
  channels: Channel[];
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const org = useOrg();
  const qc = useQueryClient();
  const projects = useQuery(ProjectService.method.listProjects, {}, { enabled: open });
  const [name, setName] = useState(rule?.name ?? "");
  const [project, setProject] = useState(rule?.project?.slug ?? "*");
  const [kind, setKind] = useState(String(rule?.trigger?.kind ?? TriggerKind.NEW_ISSUE));
  const [threshold, setThreshold] = useState(String(rule?.trigger?.threshold || 50));
  const [window, setWindow] = useState(String(seconds(rule?.trigger?.window) || 3600));
  const [level, setLevel] = useState(String(rule?.filters?.minLevel ?? Level.ERROR));
  const [envs, setEnvs] = useState(rule?.filters?.environments.join(", ") ?? "production");
  const [customers, setCustomers] = useState(rule?.filters?.customers.join(", ") ?? "");
  const [unhandled, setUnhandled] = useState(rule?.filters?.unhandledOnly ?? false);
  const [selected, setSelected] = useState<string[]>(rule?.channelIds ?? channels.map((c) => c.id));
  const [throttle, setThrottle] = useState(String(rule ? seconds(rule.throttle) : 1800));
  const done = () => {
    toast.success(rule ? "Kural güncellendi" : "Kural eklendi");
    void qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: AlertService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
    onOpenChange(false);
  };
  const createRule = useMutation(AlertService.method.createAlertRule, { onSuccess: done });
  const updateRule = useMutation(AlertService.method.updateAlertRule, { onSuccess: done });
  const error = createRule.error ?? updateRule.error;
  const pending = createRule.isPending || updateRule.isPending;
  const dur = (s: string) => create(DurationSchema, { seconds: BigInt(Number(s)) });

  const submit = () => {
    const input = {
      name,
      project: project === "*" ? "" : project,
      enabled: rule?.enabled ?? true,
      trigger: {
        kind: Number(kind) as TriggerKind,
        threshold: Number(kind) === TriggerKind.FREQUENCY ? Number(threshold) : 0,
        window: Number(kind) === TriggerKind.FREQUENCY ? dur(window) : undefined,
      },
      filters: {
        minLevel: Number(level) as Level,
        environments: csv(envs),
        customers: csv(customers),
        unhandledOnly: unhandled,
      },
      channelIds: selected,
      throttle: dur(throttle),
    };
    if (rule) updateRule.mutate({ id: rule.id, rule: input });
    else createRule.mutate({ rule: input });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{rule ? "Kuralı düzenle" : "Yeni kural"}</DialogTitle>
          <DialogDescription>
            Hangi durumda, hangi hataların, hangi kanallara bildirileceğini seçin.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Field label="Kural adı" htmlFor="rule-name">
            <Input
              id="rule-name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Üretimde yeni hatalar"
            />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Ne zaman" htmlFor="rule-kind">
              <Select value={kind} onValueChange={setKind}>
                <SelectTrigger id="rule-kind" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={String(TriggerKind.NEW_ISSUE)}>Yeni bir hata çıktığında</SelectItem>
                  <SelectItem value={String(TriggerKind.REGRESSION)}>
                    Çözülmüş hata geri döndüğünde
                  </SelectItem>
                  <SelectItem value={String(TriggerKind.FREQUENCY)}>Hata sıklaştığında</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label="Proje" htmlFor="rule-project">
              <Select value={project} onValueChange={setProject}>
                <SelectTrigger id="rule-project" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="*">Tüm projeler</SelectItem>
                  {projects.data?.projects.map((p) => (
                    <SelectItem key={p.id} value={p.slug}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>
          {Number(kind) === TriggerKind.FREQUENCY ? (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                label="Olay sayısı"
                htmlFor="rule-threshold"
                error={errorField(error) === "rule.trigger" ? errorMessage(error) : undefined}
              >
                <Input
                  id="rule-threshold"
                  type="number"
                  min={1}
                  required
                  value={threshold}
                  onChange={(e) => setThreshold(e.target.value)}
                />
              </Field>
              <Field label="Süre içinde" htmlFor="rule-window">
                <Select value={window} onValueChange={setWindow}>
                  <SelectTrigger id="rule-window" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {windows.map((w) => (
                      <SelectItem key={w.value} value={w.value}>
                        {w.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          ) : null}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Seviye" htmlFor="rule-level">
              <Select value={level} onValueChange={setLevel}>
                <SelectTrigger id="rule-level" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {levelsFilter.map((l) => (
                    <SelectItem key={l.value} value={l.value}>
                      {l.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Ortamlar" htmlFor="rule-envs" hint="Virgülle ayırın; boş: tüm ortamlar.">
              <Input id="rule-envs" value={envs} onChange={(e) => setEnvs(e.target.value)} />
            </Field>
          </div>
          <Field
            label="Müşteriler"
            htmlFor="rule-customers"
            hint="Müşteri anahtarları, virgülle ayırın; boş: tüm müşteriler."
          >
            <Input
              id="rule-customers"
              value={customers}
              onChange={(e) => setCustomers(e.target.value)}
              placeholder="sivas-belediyesi"
            />
          </Field>
          <div className="flex items-center gap-2">
            <Checkbox
              id="rule-unhandled"
              checked={unhandled}
              onCheckedChange={(v) => setUnhandled(v === true)}
            />
            <Label htmlFor="rule-unhandled" className="font-normal text-sm">
              Yalnız işlenmemiş istisnalar
            </Label>
          </div>
          <fieldset className="grid gap-2">
            <legend className="mb-1 font-medium text-sm">Bildirilecek kanallar</legend>
            {channels.map((c) => (
              <div key={c.id} className="flex items-center gap-2">
                <Checkbox
                  id={`rule-ch-${c.id}`}
                  checked={selected.includes(c.id)}
                  onCheckedChange={(v) =>
                    setSelected((s) => (v === true ? [...s, c.id] : s.filter((x) => x !== c.id)))
                  }
                />
                <Label htmlFor={`rule-ch-${c.id}`} className="font-normal text-sm">
                  {c.name}
                </Label>
              </div>
            ))}
          </fieldset>
          <Field label="Aynı hata için en fazla" htmlFor="rule-throttle">
            <Select value={throttle} onValueChange={setThrottle}>
              <SelectTrigger id="rule-throttle" className="w-full sm:w-60">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {throttles.map((t) => (
                  <SelectItem key={t.value} value={t.value}>
                    {t.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <FormError
            message={error && errorField(error) !== "rule.trigger" ? errorMessage(error) : undefined}
          />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={pending || selected.length === 0}>
              {rule ? "Kaydet" : "Kuralı ekle"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
