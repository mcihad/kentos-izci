import { clone, create } from "@bufbuild/protobuf";
import { useState } from "react";
import { Field } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  AiSettingsSchema,
  AutoAction,
  AutoAnalyze,
  type Project,
  ProjectSettingsSchema,
} from "@/gen/izci/api/v1/project_pb";
import { Level } from "@/gen/izci/ingest/v1/event_pb";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { lines, useUpdateProject } from "./use-project";

const actions = [
  { value: AutoAction.NONE, label: "Yalnız analiz et" },
  { value: AutoAction.BRANCH, label: "Düzeltmeyi bir dala gönder" },
  { value: AutoAction.DRAFT_PULL_REQUEST, label: "Taslak çekme isteği aç" },
  { value: AutoAction.PULL_REQUEST, label: "Çekme isteği aç" },
];

export function ProjectAi({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const update = useUpdateProject();
  const ai = project.settings?.ai;
  const [auto, setAuto] = useState<AutoAnalyze>(ai?.autoAnalyze || AutoAnalyze.OFF);
  const [minLevel, setMinLevel] = useState<Level>(ai?.minLevel || Level.ERROR);
  const [action, setAction] = useState<AutoAction>(ai?.autoAction || AutoAction.NONE);
  const [budget, setBudget] = useState(ai?.dailyBudget ? String(ai.dailyBudget) : "");
  const [model, setModel] = useState(ai?.model ?? "");
  const [commands, setCommands] = useState(ai?.allowedCommands.join("\n") ?? "");
  const [instructions, setInstructions] = useState(ai?.instructions ?? "");

  return (
    <form
      className="grid max-w-2xl grid-cols-1 gap-5 py-6"
      onSubmit={(e) => {
        e.preventDefault();
        const settings = clone(ProjectSettingsSchema, project.settings ?? create(ProjectSettingsSchema));
        settings.ai = create(AiSettingsSchema, {
          autoAnalyze: auto,
          minLevel,
          autoAction: action,
          dailyBudget: Number(budget) || 0,
          model: model.trim(),
          allowedCommands: lines(commands),
          instructions: instructions.trim(),
        });
        update.mutate({ slug: project.slug, settings });
      }}
    >
      <div>
        <h2 className="font-semibold text-[15px]">Yapay zekâ ile analiz</h2>
        <p className="mt-0.5 text-[13px] text-muted-foreground leading-relaxed">
          İzci, bağlı depodaki kodu opencode ile inceler, kök nedeni açıklar ve bir düzeltme önerir. Otomatik
          analiz, yeni hatalar için bu adımı sizin yerinize başlatır.
        </p>
      </div>
      <Field label="Otomatik analiz" htmlFor="ai-auto">
        <Select
          value={String(auto)}
          onValueChange={(v) => setAuto(Number(v) as AutoAnalyze)}
          disabled={!admin}
        >
          <SelectTrigger id="ai-auto" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={String(AutoAnalyze.OFF)}>Kapalı, yalnız elle başlatılır</SelectItem>
            <SelectItem value={String(AutoAnalyze.NEW_ISSUES)}>Yeni hatalarda otomatik başlat</SelectItem>
          </SelectContent>
        </Select>
      </Field>
      {auto === AutoAnalyze.NEW_ISSUES ? (
        <>
          <Field label="En düşük seviye" htmlFor="ai-level">
            <Select
              value={String(minLevel)}
              onValueChange={(v) => setMinLevel(Number(v) as Level)}
              disabled={!admin}
            >
              <SelectTrigger id="ai-level" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={String(Level.WARNING)}>Uyarı ve üstü</SelectItem>
                <SelectItem value={String(Level.ERROR)}>Hata ve üstü</SelectItem>
                <SelectItem value={String(Level.FATAL)}>Yalnız ölümcül</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="Analizden sonra" htmlFor="ai-action">
            <Select
              value={String(action)}
              onValueChange={(v) => setAction(Number(v) as AutoAction)}
              disabled={!admin}
            >
              <SelectTrigger id="ai-action" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {actions.map((a) => (
                  <SelectItem key={a.value} value={String(a.value)}>
                    {a.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="Günlük analiz sınırı" htmlFor="ai-budget" hint="Boş ya da 0: sınırsız.">
            <Input
              id="ai-budget"
              type="number"
              min={0}
              max={1000}
              value={budget}
              onChange={(e) => setBudget(e.target.value)}
              disabled={!admin}
              className="w-40"
            />
          </Field>
        </>
      ) : null}
      <Field label="Model" htmlFor="ai-model" hint="Boş bırakırsanız sunucunun varsayılan modeli kullanılır.">
        <Input
          id="ai-model"
          value={model}
          onChange={(e) => setModel(e.target.value)}
          disabled={!admin}
          placeholder="anthropic/claude-sonnet-5"
          className="font-mono text-[13px]"
        />
      </Field>
      <Field
        label="Çalıştırılabilecek komutlar"
        htmlFor="ai-commands"
        hint="Ajanın düzeltmeyi doğrulamak için çalıştırabileceği komutlar, her satıra bir tane. Bu komutlar deponun kodunu sunucuda çalıştırır; yalnız güvendiğiniz depolar için ekleyin. Boş bırakılırsa ajan hiçbir komut çalıştıramaz."
      >
        <Textarea
          id="ai-commands"
          value={commands}
          onChange={(e) => setCommands(e.target.value)}
          disabled={!admin}
          className="min-h-20 font-mono text-[12.5px]"
          placeholder={"dotnet build\ndotnet test"}
        />
      </Field>
      <Field
        label="Proje talimatları"
        htmlFor="ai-instructions"
        hint="Kod stili, mimari kurallar, dokunulmaması gereken yerler…"
      >
        <Textarea
          id="ai-instructions"
          value={instructions}
          onChange={(e) => setInstructions(e.target.value)}
          disabled={!admin}
          className="min-h-28 text-[13px]"
        />
      </Field>
      {admin ? (
        <div>
          <Button type="submit" disabled={update.isPending}>
            Kaydet
          </Button>
        </div>
      ) : null}
    </form>
  );
}
