import { clone, create } from "@bufbuild/protobuf";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { Field } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Textarea } from "@kentos/components/ui/textarea";
import { FlaskConicalIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { EventPick, EventService } from "@/gen/izci/api/v1/event_pb";
import { IssueService } from "@/gen/izci/api/v1/issue_pb";
import {
  type GroupingRuleProblem,
  GroupingRuleProblemKind,
  GroupingSettingsSchema,
  type Project,
  ProjectService,
  ProjectSettingsSchema,
  type TestGroupingRulesResponse,
} from "@/gen/izci/api/v1/project_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { useUpdateProject } from "./use-project";

const fingerprintExample = `# Eşleşen ilk kural olayın parmak izini belirler.
error.type:*TimeoutException transaction:/api/odeme* -> odeme-zaman-asimi
error.value:"*bağlantı havuzu*" -> baglanti-havuzu
message:"Rapor * oluşturulamadı" -> rapor-olusturma
tags.modul:imar -> {{ default }}, imar`;

const stacktraceExample = `# Çerçeveleri gruplamaya kat (+group) ya da dışarıda bırak (-group);
# uygulama kodu say (+app) ya da sayma (-app).
stack.function:*Logger.* -group
stack.module:Kalem.Ortak.* -app
stack.module:Polly* +group`;

/** A rules problem in Turkish. */
function problemText(p: GroupingRuleProblem): string {
  const where = `${p.field === "fingerprint_rules" ? "Parmak izi" : "Yığın izi"} kuralları, ${p.line}. satır`;
  const what: Record<number, string> = {
    [GroupingRuleProblemKind.NO_ARROW]: "eşleştiriciler, “->” ve parmak izi bekleniyor",
    [GroupingRuleProblemKind.SYNTAX]: `“${p.value}” anahtar:değer biçiminde değil`,
    [GroupingRuleProblemKind.UNKNOWN_MATCHER]: `bilinmeyen eşleştirici “${p.value}”`,
    [GroupingRuleProblemKind.UNKNOWN_VARIABLE]: `bilinmeyen değişken {{ ${p.value} }}`,
    [GroupingRuleProblemKind.DEFAULT_NOT_ALONE]: "{{ default }} parmak izinin ayrı bir parçası olmalı",
    [GroupingRuleProblemKind.EMPTY_FINGERPRINT]: "“->” sonrası parmak izi boş",
    [GroupingRuleProblemKind.UNKNOWN_ACTION]: `bilinmeyen eylem “${p.value}” (+group, -group, +app, -app)`,
    [GroupingRuleProblemKind.NO_ACTION]: "eylem yok (+group, -group, +app, -app)",
    [GroupingRuleProblemKind.NO_MATCHER]: "eşleştirici yok",
    [GroupingRuleProblemKind.UNTERMINATED_QUOTE]: `tırnak kapanmamış: ${p.value}`,
    [GroupingRuleProblemKind.TOO_MANY]: "kural, eşleştirici ya da parça sayısı sınırı aşıldı",
  };
  return `${where}: ${what[p.kind] ?? p.message}`;
}

/** Project tab: fingerprint and stack trace rules, with a test against a stored event. */
export function ProjectGrouping({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const transport = useTransport();
  const update = useUpdateProject("Gruplama kuralları kaydedildi");
  const [fingerprint, setFingerprint] = useState(project.settings?.grouping?.fingerprintRules ?? "");
  const [stacktrace, setStacktrace] = useState(project.settings?.grouping?.stacktraceRules ?? "");
  const [issue, setIssue] = useState<string>();
  const [result, setResult] = useState<TestGroupingRulesResponse>();
  const [problems, setProblems] = useState<GroupingRuleProblem[]>([]);
  const [testing, setTesting] = useState(false);
  const issues = useQuery(
    IssueService.method.listIssues,
    { query: `project:${project.slug}`, pageSize: 25 },
    { staleTime: 60_000 },
  );
  const grouping = () =>
    create(GroupingSettingsSchema, { fingerprintRules: fingerprint, stacktraceRules: stacktrace });

  const check = async (eventId: string) => {
    const res = await transport.unary(
      ProjectService.method.testGroupingRules,
      undefined,
      undefined,
      undefined,
      {
        project: project.slug,
        grouping: grouping(),
        eventId,
      },
    );
    setProblems(res.message.problems);
    return res.message;
  };

  const test = async () => {
    setTesting(true);
    try {
      let eventId = "";
      if (issue) {
        const ev = await transport.unary(EventService.method.getEvent, undefined, undefined, undefined, {
          issueId: issue,
          pick: EventPick.LATEST,
        });
        eventId = ev.message.event?.id ?? "";
      }
      setResult(await check(eventId));
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setTesting(false);
    }
  };

  return (
    <form
      className="grid max-w-3xl grid-cols-1 *:min-w-0 gap-6 py-6"
      onSubmit={(e) => {
        e.preventDefault();
        void (async () => {
          try {
            if ((await check("")).problems.length > 0) return;
          } catch (err) {
            toast.error(errorMessage(err));
            return;
          }
          const settings = clone(ProjectSettingsSchema, project.settings ?? create(ProjectSettingsSchema));
          settings.grouping = grouping();
          update.mutate({ slug: project.slug, settings });
        })();
      }}
    >
      <div className="max-w-2xl">
        <h2 className="font-semibold text-lg">Gruplama kuralları</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          Olayların hangi hatada toplanacağını değiştirir. Kurallar yalnız yeni olaylara uygulanır; mevcut
          hatalar parmak izlerini korur. Yanlış ayrılmış hataları hata listesinden seçip birleştirebilirsiniz.
          Değerler büyük/küçük harf ayırmaz; <code className="font-mono text-xs">*</code> her şeyle,{" "}
          <code className="font-mono text-xs">?</code> tek karakterle eşleşir, başına{" "}
          <code className="font-mono text-xs">!</code> koyulan eşleştirici tersine döner.
        </p>
      </div>

      <Field
        label="Parmak izi kuralları"
        htmlFor="fp-rules"
        hint="Eşleştiriciler: error.type, error.value, message, logger, level, transaction, environment, release, tags.<ad>, stack.module, stack.function, stack.filename. Parmak izinde virgülle ayrılan parçalar ve {{ default }}, {{ error.type }}, {{ error.value }}, {{ message }}, {{ transaction }}, {{ stack.function }}, {{ tags.<ad> }} gibi değişkenler kullanılabilir. Eşleşen kural uygulamanın gönderdiği parmak izinden önce gelir."
      >
        <Textarea
          id="fp-rules"
          value={fingerprint}
          onChange={(e) => setFingerprint(e.target.value)}
          disabled={!admin}
          spellCheck={false}
          placeholder={fingerprintExample}
          className="min-h-40 font-mono text-sm"
        />
      </Field>
      <Field
        label="Yığın izi kuralları"
        htmlFor="st-rules"
        hint="Eşleştiriciler: stack.module, stack.function, stack.filename, stack.package. Eylemler: +group ve -group çerçevenin gruplamaya katılıp katılmayacağını, +app ve -app uygulama kodu sayılıp sayılmayacağını belirler (SDK'nın işaretini de geçersiz kılar). Birden çok kural eşleşirse sonuncusu geçerlidir."
      >
        <Textarea
          id="st-rules"
          value={stacktrace}
          onChange={(e) => setStacktrace(e.target.value)}
          disabled={!admin}
          spellCheck={false}
          placeholder={stacktraceExample}
          className="min-h-32 font-mono text-sm"
        />
      </Field>

      {problems.length > 0 ? (
        <ul className="grid gap-1 rounded-md border border-destructive/35 px-4 py-3 text-sm" role="alert">
          {problems.map((p) => (
            <li key={`${p.field}-${p.line}`}>{problemText(p)}</li>
          ))}
        </ul>
      ) : null}

      <section className="grid gap-3 rounded-lg border px-4 py-4">
        <div>
          <h3 className="font-semibold text-md">Dene</h3>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Kuralları kaydetmeden önce bir hatanın son olayına uygulayın: hangi parmak izini alacağını ve
            hangi hataya düşeceğini görün.
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <Select value={issue} onValueChange={setIssue}>
            <SelectTrigger className="w-full sm:w-96" aria-label="Örnek hata">
              <SelectValue placeholder="Bir hata seçin" />
            </SelectTrigger>
            <SelectContent className="max-h-72">
              {issues.data?.issues.map((i) => (
                <SelectItem key={i.id} value={i.id}>
                  <span className="font-mono text-xs">{i.shortId}</span> {i.title}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button type="button" variant="outline" disabled={testing} onClick={() => void test()}>
            <FlaskConicalIcon className="size-4" />
            {issue ? "Dene" : "Kuralları denetle"}
          </Button>
        </div>
        {result?.grouping ? (
          <div className="grid gap-1.5 text-sm">
            <p>
              {result.grouping.strategy === "rule" ? (
                <>
                  Kural eşleşti:{" "}
                  <code className="rounded bg-(--surface-muted) px-1 font-mono text-xs">
                    {result.grouping.rule}
                  </code>
                </>
              ) : (
                "Hiçbir parmak izi kuralı eşleşmedi; varsayılan gruplama kullanılır."
              )}
            </p>
            <ul className="grid gap-0.5 font-mono text-muted-foreground text-xs">
              {result.grouping.components.slice(0, 12).map((c) => (
                <li key={c} className="truncate" title={c}>
                  {c}
                </li>
              ))}
            </ul>
            <p className="text-muted-foreground">
              {result.sameIssue
                ? "Olay bugünkü hatasında kalır."
                : result.issueShortId
                  ? `Bu kurallarla benzer olaylar ${result.issueShortId} hatasına düşer.`
                  : "Bu kurallarla benzer olaylar yeni bir hata açar."}
            </p>
          </div>
        ) : result && problems.length === 0 ? (
          <p className="text-sm text-muted-foreground">Kurallarda sorun yok.</p>
        ) : null}
      </section>

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
