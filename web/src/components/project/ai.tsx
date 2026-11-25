import { clone, create } from "@bufbuild/protobuf";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { Field } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { AiService } from "@/gen/izci/api/v1/ai_pb";
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
];

export function ProjectAi({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const update = useUpdateProject();
  const ai = project.settings?.ai;
  const [auto, setAuto] = useState<AutoAnalyze>(ai?.autoAnalyze || AutoAnalyze.OFF);
  const [minLevel, setMinLevel] = useState<Level>(ai?.minLevel || Level.ERROR);
  // Automatic pull requests are always drafts (PULL_REQUEST is a legacy value).
  const [action, setAction] = useState<AutoAction>(
    ai?.autoAction === AutoAction.PULL_REQUEST
      ? AutoAction.DRAFT_PULL_REQUEST
      : ai?.autoAction || AutoAction.NONE,
  );
  const [budget, setBudget] = useState(ai?.dailyBudget ? String(ai.dailyBudget) : "10");
  const [model, setModel] = useState(ai?.model ?? "");
  const [commands, setCommands] = useState(ai?.allowedCommands.join("\n") ?? "");
  const [instructions, setInstructions] = useState(ai?.instructions ?? "");
  // The server decides which commands an agent may run at all.
  const status = useQuery(AiService.method.getAiStatus, {}, { staleTime: 60_000 });
  const serverCommands = status.data?.allowedCommands ?? [];
  const refused = lines(commands).filter((c) => !permitted(c, serverCommands));

  return (
    <form
      className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-5 py-6"
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
        <h2 className="font-semibold text-lg">Yapay zekâ ile analiz</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
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
          <Field
            label="Analizden sonra"
            htmlFor="ai-action"
            hint="Otomatik değişiklikler izci-auto/ ile başlayan dallara gider ve çekme istekleri hep taslak açılır. CI dosyalarına, derleme betiklerine ya da bağımlılık dosyalarına dokunan değişiklikler otomatik gönderilmez. CI'ı izci-auto/ dallarında sırlarla çalışmayacak şekilde ayarlayın."
          >
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
          <Field
            label="Günlük analiz sınırı"
            htmlFor="ai-budget"
            hint="Günde en çok bu kadar otomatik analiz başlatılır; 0 ise otomatik analiz yapılmaz."
          >
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
          className="font-mono text-sm"
        />
      </Field>
      <Field
        label="Çalıştırılabilecek komutlar"
        htmlFor="ai-commands"
        error={
          refused.length
            ? `Sunucu şu komutlara izin vermiyor: ${refused.join(", ")}. Sunucu yöneticisinden AI_ALLOWED_COMMANDS listesine eklemesini isteyin.`
            : undefined
        }
        hint={
          serverCommands.length
            ? `Ajanın düzeltmeyi doğrulamak için çalıştırabileceği komutlar, her satıra bir tane. Sunucunun izin verdikleri: ${serverCommands.join(", ")} (argüman eklenebilir).${status.data?.sandbox === "bwrap" ? " Komutlar yalıtılmış bir ortamda çalışır." : " Komutlar deponun kodunu sunucuda, yalıtılmadan çalıştırır; yalnız güvendiğiniz depolar için ekleyin."}`
            : "Bu sunucuda ajan hiçbir komut çalıştıramaz; sunucu yöneticisi AI_ALLOWED_COMMANDS ile izin verebilir."
        }
      >
        <Textarea
          id="ai-commands"
          value={commands}
          onChange={(e) => setCommands(e.target.value)}
          disabled={!admin || (serverCommands.length === 0 && commands.trim() === "")}
          className="min-h-20 font-mono text-sm"
          placeholder={serverCommands.length ? serverCommands.join("\n") : undefined}
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
          className="min-h-28 text-sm"
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

/** Whether the server's allowlist covers a project command ("go test ./..." under "go test"). */
function permitted(cmd: string, server: string[]): boolean {
  const c = cmd.trim().split(/\s+/).join(" ");
  return server.some((s) => {
    const p = s.trim().split(/\s+/).join(" ");
    return p !== "" && (c === p || c.startsWith(`${p} `));
  });
}
