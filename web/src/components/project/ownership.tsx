import { create } from "@bufbuild/protobuf";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
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
import { Label } from "@kentos/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@kentos/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { Textarea } from "@kentos/components/ui/textarea";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { XIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { OrganizationService } from "@/gen/izci/api/v1/organization_pb";
import {
  AutoAssign,
  GetOwnershipResponseSchema,
  type Ownership,
  OwnershipService,
  type RuleProblem,
  RuleProblemKind,
} from "@/gen/izci/api/v1/ownership_pb";
import type { Project } from "@/gen/izci/api/v1/project_pb";
import { errorDetail, errorField, errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";

const autoOptions = [
  { value: AutoAssign.OFF, label: "Kapalı", hint: "Yeni hatalar atanmadan kalır." },
  {
    value: AutoAssign.OWNERS,
    label: "Sahiplik kurallarına göre",
    hint: "Yeni hata, eşleşen son kuralın gösterdiği ilk üyeye atanır; bildirimde atanan kişi de yazar.",
  },
  {
    value: AutoAssign.SUSPECT_COMMIT,
    label: "Şüpheli commit'in yazarına",
    hint: "Hatalı satırları en son değiştiren commit'in yazarı üyeyse ona, değilse kurallara göre atanır. GitHub bağlantılı depo gerekir.",
  },
];

const syntax = [
  { rule: "path:src/odeme/*", text: "Yığın izindeki dosya yolu; göreli desen her klasörde eşleşir." },
  { rule: "module:Kentos.Odeme.*", text: "Çerçevenin modülü ya da ad alanı." },
  { rule: "url:*/odeme/*", text: "İsteğin adresi." },
  {
    rule: "tags.customer:sivas",
    text: "Olay etiketi; level, environment, release, customer ve tenant da kullanılabilir.",
  },
  { rule: "codeowners:/src/odeme/", text: "Depodaki yol, CODEOWNERS kalıbıyla." },
];

/** A problem on a rule line, in Turkish. */
function problemText(kind: RuleProblemKind, value: string): string {
  switch (kind) {
    case RuleProblemKind.SYNTAX:
      return "satır “tür:desen sahip…” biçiminde değil";
    case RuleProblemKind.UNKNOWN_TYPE:
      return `bilinmeyen kural türü “${value}”; path, module, url, tags.<anahtar> ya da codeowners olmalı`;
    case RuleProblemKind.NO_OWNERS:
      return "kuralda sahip yok";
    case RuleProblemKind.INVALID_OWNER:
      return `“${value}” bir e-posta adresi, @ad ya da #ekip değil`;
    case RuleProblemKind.INVALID_PATTERN:
      return `“${value}” deseni geçersiz`;
    case RuleProblemKind.TOO_MANY:
      return "çok fazla kural ya da bir satırda 20'den fazla sahip var";
    default:
      return "satır geçersiz";
  }
}

/** The rule line an UpdateOwnership error points at. */
function ruleError(err: unknown): string | undefined {
  if (!err) return undefined;
  if (errorField(err) !== "rules") return errorMessage(err);
  const md = errorDetail(err)?.metadata ?? {};
  const name = (md.kind ?? "").replace("RULE_PROBLEM_KIND_", "") as keyof typeof RuleProblemKind;
  return `Satır ${md.line}: ${problemText(RuleProblemKind[name] ?? RuleProblemKind.UNSPECIFIED, md.value ?? "")}.`;
}

function Problems({ problems }: { problems: RuleProblem[] }) {
  if (problems.length === 0) return null;
  return (
    <ul className="grid gap-1 rounded-md border border-(--status-warning-border) bg-(--status-warning-bg) px-3 py-2 text-(--status-warning-fg) text-sm">
      {problems.slice(0, 8).map((p) => (
        <li key={`${p.field}-${p.line}`}>
          {p.field === "codeowners" ? "CODEOWNERS" : "Kurallar"}, satır {p.line}:{" "}
          {problemText(p.kind, p.value)} (atlanıyor)
        </li>
      ))}
      {problems.length > 8 ? <li>ve {problems.length - 8} satır daha</li> : null}
    </ul>
  );
}

/** Ownership rules, CODEOWNERS and automatic assignment of a project. */
export function ProjectOwnership({ project }: { project: Project }) {
  const q = useQuery(OwnershipService.method.getOwnership, { project: project.slug });
  const ownership = q.data?.ownership;
  if (!ownership) return <Skeleton className="my-6 h-72 w-full max-w-3xl" />;
  return <OwnershipForm project={project} ownership={ownership} />;
}

function OwnershipForm({ project, ownership }: { project: Project; ownership: Ownership }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const qc = useQueryClient();
  const [rules, setRules] = useState(ownership.rules);
  const [pasting, setPasting] = useState(false);
  const key = createConnectQueryKey({
    schema: OwnershipService.method.getOwnership,
    input: { project: project.slug },
    transport: org.transport,
    cardinality: "finite",
  });
  const saved = (o: Ownership | undefined, message: string) => {
    if (o) qc.setQueryData(key, create(GetOwnershipResponseSchema, { ownership: o }));
    toast.success(message);
  };
  const update = useMutation(OwnershipService.method.updateOwnership);
  const imp = useMutation(OwnershipService.method.importCodeowners, {
    onSuccess: (res) => saved(res.ownership, "CODEOWNERS içe aktarıldı"),
    onError: (err) => toast.error(errorMessage(err)),
  });
  const auto = ownership.autoAssign === AutoAssign.UNSPECIFIED ? AutoAssign.OFF : ownership.autoAssign;
  const co = ownership.codeowners;

  return (
    <div className="grid max-w-3xl grid-cols-1 *:min-w-0 gap-10 py-6">
      <section className="grid gap-3">
        <div>
          <h2 className="font-semibold text-lg">Otomatik atama</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Bu projede ilk kez görülen hatalar kime atansın?
          </p>
        </div>
        <RadioGroup
          value={String(auto)}
          disabled={!admin || update.isPending}
          onValueChange={(v) =>
            update.mutate(
              { project: project.slug, autoAssign: Number(v) as AutoAssign },
              {
                onSuccess: (res) => saved(res.ownership, "Otomatik atama kaydedildi"),
                onError: (err) => toast.error(errorMessage(err)),
              },
            )
          }
          className="gap-2.5"
        >
          {autoOptions.map((o) => (
            <div key={o.value} className="flex items-start gap-2.5">
              <RadioGroupItem
                value={String(o.value)}
                id={`auto-${o.value}`}
                className="mt-0.5"
                disabled={
                  o.value === AutoAssign.SUSPECT_COMMIT && !ownership.githubLinked && auto !== o.value
                }
              />
              <Label htmlFor={`auto-${o.value}`} className="grid gap-0.5 font-normal">
                <span className="text-sm">{o.label}</span>
                <span className="text-xs text-muted-foreground leading-relaxed">{o.hint}</span>
              </Label>
            </div>
          ))}
        </RadioGroup>
      </section>

      <form
        className="grid gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          update.mutate(
            { project: project.slug, rules },
            {
              onSuccess: (res) => saved(res.ownership, "Sahiplik kuralları kaydedildi"),
              onError: (err) => {
                if (errorField(err) !== "rules") toast.error(errorMessage(err));
              },
            },
          );
        }}
      >
        <div>
          <h2 className="font-semibold text-lg">Sahiplik kuralları</h2>
          <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
            Her satırda bir kural ve ardından sahipleri: e-posta adresi, <code>@kullanıcı</code>,{" "}
            <code>@kuruluş/ekip</code> ya da <code>#ekip</code>. Kurallar yukarıdan aşağı okunur ve eşleşen
            son kural atamayı belirler; CODEOWNERS satırları bu kurallardan önce okunur.
          </p>
        </div>
        <Field
          label="Kurallar"
          htmlFor="ownership-rules"
          error={update.error && errorField(update.error) === "rules" ? ruleError(update.error) : undefined}
        >
          <Textarea
            id="ownership-rules"
            rows={8}
            className="font-mono text-sm"
            spellCheck={false}
            disabled={!admin}
            value={rules}
            onChange={(e) => setRules(e.target.value)}
            placeholder={
              "# Tahsilat ekibi\npath:src/Tahsilat/* ayse@kentos.com.tr\nurl:*/odeme/* @tahsilat-ekibi\ntags.customer:sivas mehmet@kentos.com.tr"
            }
          />
        </Field>
        <dl className="grid grid-cols-1 gap-x-4 gap-y-1.5 text-sm sm:grid-cols-[auto_minmax(0,1fr)]">
          {syntax.map((s) => (
            <div key={s.rule} className="contents">
              <dt className="font-mono text-xs leading-5">{s.rule}</dt>
              <dd className="text-muted-foreground max-sm:mb-1.5">{s.text}</dd>
            </div>
          ))}
        </dl>
        {admin ? (
          <div>
            <Button type="submit" disabled={update.isPending || rules === ownership.rules}>
              Kuralları kaydet
            </Button>
          </div>
        ) : null}
      </form>

      <section className="grid gap-3">
        <div>
          <h2 className="font-semibold text-lg">CODEOWNERS</h2>
          <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
            Depodaki CODEOWNERS dosyası, yolların sahiplerini ekiplerin zaten tuttuğu yerden alır. Yığın
            izindeki yollar, depo bağlantısının yol eşlemeleriyle depo yollarına çevrilir.
          </p>
        </div>
        {co ? (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
            <div className="min-w-0">
              <p className="truncate font-mono text-sm">{co.source || "Yapıştırılan dosya"}</p>
              <p className="text-sm text-muted-foreground">
                {co.ruleCount} kural
                {co.syncedAt ? (
                  <>
                    , <RelativeTime value={co.syncedAt} /> alındı
                  </>
                ) : null}
              </p>
            </div>
            {admin ? (
              <div className="flex flex-wrap gap-2">
                {co.source && ownership.githubLinked ? (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={imp.isPending}
                    onClick={() => imp.mutate({ project: project.slug })}
                  >
                    GitHub'dan yenile
                  </Button>
                ) : null}
                <Button variant="ghost" size="sm" onClick={() => setPasting(true)}>
                  Değiştir
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-destructive"
                  disabled={update.isPending}
                  onClick={() =>
                    update.mutate(
                      { project: project.slug, codeowners: "" },
                      {
                        onSuccess: (res) => saved(res.ownership, "CODEOWNERS kaldırıldı"),
                        onError: (err) => toast.error(errorMessage(err)),
                      },
                    )
                  }
                >
                  Kaldır
                </Button>
              </div>
            ) : null}
          </div>
        ) : admin ? (
          <div className="grid gap-2">
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                disabled={!ownership.githubLinked || imp.isPending}
                onClick={() => imp.mutate({ project: project.slug })}
              >
                GitHub'dan içe aktar
              </Button>
              <Button variant="ghost" onClick={() => setPasting(true)}>
                Yapıştır
              </Button>
            </div>
            {!ownership.githubLinked ? (
              <p className="text-sm text-muted-foreground">
                İçe aktarmak için projeye GitHub bağlantısıyla bir{" "}
                <Link
                  to="/o/$org/projects/$slug"
                  params={{ org: org.slug, slug: project.slug }}
                  search={{ tab: "repository" }}
                  className="font-medium text-foreground underline-offset-4 hover:underline"
                >
                  depo bağlayın
                </Link>
                ; bağlı değilse dosyayı yapıştırabilirsiniz.
              </p>
            ) : null}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">CODEOWNERS eklenmemiş.</p>
        )}
        <Problems problems={ownership.problems} />
      </section>

      <OwnerAliases unmapped={ownership.unmappedOwners} admin={admin} />

      <PasteDialog
        open={pasting}
        initial={co?.content ?? ""}
        onClose={() => setPasting(false)}
        onSave={(text) =>
          update.mutate(
            { project: project.slug, codeowners: text },
            {
              onSuccess: (res) => {
                saved(res.ownership, "CODEOWNERS kaydedildi");
                setPasting(false);
              },
              onError: (err) => toast.error(errorMessage(err)),
            },
          )
        }
        pending={update.isPending}
      />
    </div>
  );
}

function PasteDialog({
  open,
  initial,
  pending,
  onClose,
  onSave,
}: {
  open: boolean;
  initial: string;
  pending: boolean;
  onClose: () => void;
  onSave: (text: string) => void;
}) {
  const [text, setText] = useState(initial);
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (v) setText(initial);
        else onClose();
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>CODEOWNERS yapıştır</DialogTitle>
          <DialogDescription>
            GitHub'a bağlı olmayan depolar için dosyanın içeriğini yapıştırın. Geçersiz satırlar atlanır.
          </DialogDescription>
        </DialogHeader>
        <Textarea
          rows={12}
          className="font-mono text-sm"
          spellCheck={false}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={
            "*                @kentos/platform\n/src/Tahsilat/   @kentos/tahsilat ayse@kentos.com.tr"
          }
        />
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Vazgeç
          </Button>
          <Button disabled={pending} onClick={() => onSave(text)}>
            Kaydet
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Aliases map "@login", "@org/team" and "#team" owners to members; unmapped
 * owners of the rules come first.
 */
function OwnerAliases({ unmapped, admin }: { unmapped: string[]; admin: boolean }) {
  const org = useOrg();
  const qc = useQueryClient();
  const aliases = useQuery(OwnershipService.method.listOwnerAliases, {});
  const members = useQuery(OrganizationService.method.listMembers, {}, { enabled: admin, staleTime: 60_000 });
  const set = useMutation(OwnershipService.method.setOwnerAlias, {
    onSuccess: (res) => {
      toast.success(res.alias ? `${res.alias.alias} eşlendi` : "Eşleme kaldırıldı");
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: OwnershipService,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const list = aliases.data?.aliases ?? [];
  const pending = unmapped.filter((o) => o.startsWith("@") || o.startsWith("#"));
  const strangers = unmapped.filter((o) => !o.startsWith("@") && !o.startsWith("#"));
  if (list.length === 0 && unmapped.length === 0) return null;
  const memberOptions = members.data?.members ?? [];

  return (
    <section className="grid gap-3">
      <div>
        <h2 className="font-semibold text-lg">Sahip eşlemeleri</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          Kurallardaki <code>@ad</code> ve <code>#ekip</code> sahiplerinin hangi üyeler olduğunu söyler.
          Eşlemeler organizasyonun bütün projelerinde geçerlidir. Bir GitHub kullanıcı adını (<code>@ad</code>
          ) eşlemek, şüpheli commit'lerin yazarını da tanıtır.
        </p>
      </div>
      {pending.length > 0 || list.length > 0 ? (
        <ul className="divide-y rounded-lg border">
          {pending.map((alias) => (
            <li key={alias} className="flex flex-wrap items-center gap-3 px-4 py-2.5">
              <span className="min-w-0 flex-[1_1_10rem] truncate font-mono text-sm">{alias}</span>
              <span className="text-(--status-warning-fg) text-sm">eşlenmemiş</span>
              {admin ? (
                <Select onValueChange={(id) => set.mutate({ alias, userIds: [id] })} disabled={set.isPending}>
                  <SelectTrigger size="sm" className="w-48 text-sm" aria-label={`${alias} için üye`}>
                    <SelectValue placeholder="Üye seçin" />
                  </SelectTrigger>
                  <SelectContent align="end">
                    {memberOptions.map((m) => (
                      <SelectItem key={m.user?.id} value={m.user?.id ?? ""}>
                        {m.user?.name || m.user?.email}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
            </li>
          ))}
          {list.map((a) => (
            <li key={a.alias} className="flex flex-wrap items-center gap-3 px-4 py-2.5">
              <span className="min-w-0 flex-[1_1_10rem] truncate font-mono text-sm">{a.alias}</span>
              <span className="min-w-0 flex-[1_1_12rem] truncate text-sm">
                {a.users.map((u) => u.name || u.email).join(", ") || "üye kalmadı"}
              </span>
              {admin ? (
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7 text-muted-foreground"
                  aria-label={`${a.alias} eşlemesini kaldır`}
                  disabled={set.isPending}
                  onClick={() => set.mutate({ alias: a.alias, userIds: [] })}
                >
                  <XIcon className="size-3.5" />
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {strangers.length > 0 ? (
        <FormError
          message={`Şu e-posta adresleri organizasyonun üyesi değil, bu yüzden atanamaz: ${strangers.join(", ")}. Üye olarak davet edin ya da kurallardan çıkarın.`}
        />
      ) : null}
    </section>
  );
}
