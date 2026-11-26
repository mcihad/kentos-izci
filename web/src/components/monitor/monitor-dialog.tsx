import { create } from "@bufbuild/protobuf";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Field, FormError } from "@kentos/components/common/field";
import { Segmented } from "@kentos/components/common/segmented";
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
import { useState } from "react";
import {
  type Monitor,
  MonitorIntervalUnit,
  type MonitorSchedule,
  MonitorScheduleSchema,
  MonitorService,
} from "@/gen/izci/api/v1/monitor_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { describeCrontab, intervalUnitLabels } from "@/lib/cron";
import { errorMessage } from "@/lib/errors";

// Zones offered in the picker; a monitor may use any IANA zone, which is
// then added to the list.
const zones = [
  "Europe/Istanbul",
  "UTC",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Moscow",
  "Asia/Dubai",
  "America/New_York",
];

type Form = {
  project: string;
  name: string;
  slug: string;
  kind: "crontab" | "interval";
  crontab: string;
  interval: string;
  unit: MonitorIntervalUnit;
  timezone: string;
  margin: string;
  runtime: string;
  failures: string;
  recoveries: string;
};

function initial(m: Monitor | undefined, project: string): Form {
  const s = m?.schedule?.kind;
  return {
    project: m?.project?.slug ?? project,
    name: m?.name ?? "",
    slug: m?.slug ?? "",
    kind: s?.case === "interval" ? "interval" : "crontab",
    crontab: s?.case === "crontab" ? s.value : "0 3 * * *",
    interval: s?.case === "interval" ? String(s.value.value) : "1",
    unit: s?.case === "interval" ? s.value.unit : MonitorIntervalUnit.HOUR,
    timezone: m?.timezone ?? "Europe/Istanbul",
    margin: String(m?.checkinMarginMinutes ?? 5),
    runtime: String(m?.maxRuntimeMinutes ?? 30),
    failures: String(m?.failureThreshold ?? 1),
    recoveries: String(m?.recoveryThreshold ?? 1),
  };
}

const whole = (s: string) => {
  const n = Number(s);
  return Number.isInteger(n) && n > 0 ? n : 0;
};

/** Creates a monitor (monitor undefined) or edits one. */
export function MonitorDialog({
  open,
  onOpenChange,
  monitor,
  defaultProject = "",
  onSaved,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  monitor?: Monitor;
  defaultProject?: string;
  onSaved: (m: Monitor) => void;
}) {
  const projects = useQuery(ProjectService.method.listProjects, {}, { enabled: open && !monitor });
  const [form, setForm] = useState(() => initial(monitor, defaultProject));
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const createMutation = useMutation(MonitorService.method.createMonitor, {
    onSuccess: (res) => res.monitor && onSaved(res.monitor),
  });
  const updateMutation = useMutation(MonitorService.method.updateMonitor, {
    onSuccess: (res) => res.monitor && onSaved(res.monitor),
  });
  const pending = createMutation.isPending || updateMutation.isPending;
  const error = createMutation.error ?? updateMutation.error;
  const projectList = projects.data?.projects ?? [];
  const project = form.project || projectList[0]?.slug || "";
  const zoneList = zones.includes(form.timezone) ? zones : [form.timezone, ...zones];
  const reading = form.kind === "crontab" ? describeCrontab(form.crontab) : undefined;

  const schedule = (): MonitorSchedule =>
    create(MonitorScheduleSchema, {
      kind:
        form.kind === "crontab"
          ? { case: "crontab", value: form.crontab.trim() }
          : { case: "interval", value: { value: whole(form.interval), unit: form.unit } },
    });

  const submit = () => {
    const policy = {
      timezone: form.timezone,
      checkinMarginMinutes: whole(form.margin) || 1,
      maxRuntimeMinutes: whole(form.runtime) || 30,
      failureThreshold: whole(form.failures) || 1,
      recoveryThreshold: whole(form.recoveries) || 1,
    };
    if (monitor) {
      updateMutation.mutate({ id: monitor.id, name: form.name.trim(), schedule: schedule(), ...policy });
    } else {
      createMutation.mutate({
        project,
        name: form.name.trim(),
        slug: form.slug.trim(),
        schedule: schedule(),
        ...policy,
      });
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (v) setForm(initial(monitor, defaultProject));
        createMutation.reset();
        updateMutation.reset();
        onOpenChange(v);
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{monitor ? "İzleyiciyi düzenle" : "Yeni izleyici"}</DialogTitle>
          <DialogDescription>
            Görevin ne zaman çalışması gerektiğini söyleyin; zamanında çalışmazsa, hata verirse ya da uzun
            sürerse bir hata kaydı açılır.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          {monitor ? null : (
            <Field label="Proje" htmlFor="monitor-project">
              <Select value={project} onValueChange={(v) => set("project", v)}>
                <SelectTrigger id="monitor-project" className="w-full">
                  <SelectValue placeholder="Proje seçin" />
                </SelectTrigger>
                <SelectContent>
                  {projectList.map((p) => (
                    <SelectItem key={p.id} value={p.slug}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          <Field label="Ad" htmlFor="monitor-name">
            <Input
              id="monitor-name"
              required
              maxLength={100}
              placeholder="Günlük tahsilat raporu"
              value={form.name}
              onChange={(e) => set("name", e.target.value)}
            />
          </Field>
          {monitor ? null : (
            <Field
              label="Kısa ad"
              htmlFor="monitor-slug"
              hint="Görev check-in gönderirken bu adı kullanır (monitor_slug). Boş bırakırsanız addan üretilir."
            >
              <Input
                id="monitor-slug"
                maxLength={50}
                placeholder="gunluk-tahsilat"
                className="font-mono"
                value={form.slug}
                onChange={(e) => set("slug", e.target.value)}
              />
            </Field>
          )}
          <Field label="Zamanlama">
            <Segmented
              label="Zamanlama türü"
              value={form.kind}
              onChange={(v) => set("kind", v)}
              options={[
                { value: "crontab", label: "Crontab" },
                { value: "interval", label: "Aralık" },
              ]}
            />
          </Field>
          {form.kind === "crontab" ? (
            <Field
              label="Crontab ifadesi"
              htmlFor="monitor-crontab"
              hint={
                reading
                  ? `${reading}.`
                  : "Beş alan: dakika, saat, ayın günü, ay, haftanın günü. Örnek: 0 3 * * *"
              }
            >
              <Input
                id="monitor-crontab"
                required
                className="font-mono"
                value={form.crontab}
                onChange={(e) => set("crontab", e.target.value)}
              />
            </Field>
          ) : (
            <Field
              label="Aralık"
              htmlFor="monitor-interval"
              hint="Bir çalışmadan sonrakine kadar geçen süre."
            >
              <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] gap-2">
                <Input
                  id="monitor-interval"
                  type="number"
                  min={1}
                  inputMode="numeric"
                  value={form.interval}
                  onChange={(e) => set("interval", e.target.value)}
                />
                <Select
                  value={String(form.unit)}
                  onValueChange={(v) => set("unit", Number(v) as MonitorIntervalUnit)}
                >
                  <SelectTrigger className="w-full" aria-label="Birim">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {intervalUnitLabels.map(([u, label]) => (
                      <SelectItem key={u} value={String(u)}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </Field>
          )}
          <Field label="Saat dilimi" htmlFor="monitor-zone" hint="Crontab bu saat dilimine göre okunur.">
            <Select value={form.timezone} onValueChange={(v) => set("timezone", v)}>
              <SelectTrigger id="monitor-zone" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {zoneList.map((z) => (
                  <SelectItem key={z} value={z}>
                    {z}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field
              label="Gecikme payı (dk)"
              htmlFor="monitor-margin"
              hint="Bu kadar gecikirse kaçırılmış sayılır."
            >
              <Input
                id="monitor-margin"
                type="number"
                min={1}
                inputMode="numeric"
                value={form.margin}
                onChange={(e) => set("margin", e.target.value)}
              />
            </Field>
            <Field label="En uzun süre (dk)" htmlFor="monitor-runtime" hint="Aşarsa zaman aşımı sayılır.">
              <Input
                id="monitor-runtime"
                type="number"
                min={1}
                inputMode="numeric"
                value={form.runtime}
                onChange={(e) => set("runtime", e.target.value)}
              />
            </Field>
            <Field
              label="Hata eşiği"
              htmlFor="monitor-failures"
              hint="Art arda bu kadar sorunda kayıt açılır."
            >
              <Input
                id="monitor-failures"
                type="number"
                min={1}
                inputMode="numeric"
                value={form.failures}
                onChange={(e) => set("failures", e.target.value)}
              />
            </Field>
            <Field
              label="Toparlanma eşiği"
              htmlFor="monitor-recoveries"
              hint="Art arda bu kadar başarıda kapanır."
            >
              <Input
                id="monitor-recoveries"
                type="number"
                min={1}
                inputMode="numeric"
                value={form.recoveries}
                onChange={(e) => set("recoveries", e.target.value)}
              />
            </Field>
          </div>
          <FormError message={error ? errorMessage(error) : undefined} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={pending || !form.name.trim() || (!monitor && !project)}>
              {monitor ? "Değişiklikleri kaydet" : "İzleyici oluştur"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
