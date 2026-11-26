import { clone, create } from "@bufbuild/protobuf";
import { useQuery } from "@connectrpc/connect-query";
import { Field } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Input } from "@kentos/components/ui/input";
import { Label } from "@kentos/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@kentos/components/ui/radio-group";
import { Switch } from "@kentos/components/ui/switch";
import { Textarea } from "@kentos/components/ui/textarea";
import { useState } from "react";
import { OrganizationService } from "@/gen/izci/api/v1/organization_pb";
import {
  InAppRulesSchema,
  IpStorage,
  PrivacySettingsSchema,
  type Project,
  ProjectSettingsSchema,
} from "@/gen/izci/api/v1/project_pb";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { lines, useUpdateProject } from "./use-project";

const ipOptions = [
  { value: IpStorage.FULL, label: "Tam adres", hint: "Hatanın hangi ağdan geldiğini ayırt etmek için." },
  { value: IpStorage.ANONYMIZED, label: "Anonimleştirilmiş", hint: "Son bölüm sıfırlanır: 85.105.12.0" },
  { value: IpStorage.NONE, label: "Saklanmasın", hint: "KVKK gereği IP adresi hiç kaydedilmez." },
];

export function ProjectPrivacy({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const update = useUpdateProject();
  const s = project.settings;
  const [ip, setIp] = useState<IpStorage>(s?.privacy?.ipStorage || IpStorage.FULL);
  const [scrub, setScrub] = useState(s?.privacy?.scrubFields.join("\n") ?? "");
  const [safe, setSafe] = useState(s?.privacy?.safeFields.join("\n") ?? "");
  const [cookies, setCookies] = useState(s?.privacy?.storeCookieValues ?? false);
  const [bodies, setBodies] = useState(s?.privacy?.storeRequestBodies ?? true);
  const [attachments, setAttachments] = useState(!(s?.privacy?.discardAttachments ?? false));
  const [include, setInclude] = useState(s?.inApp?.include.join("\n") ?? "");
  const [exclude, setExclude] = useState(s?.inApp?.exclude.join("\n") ?? "");
  const [retention, setRetention] = useState(String(s?.privacy?.retentionDays ?? 0));
  // Projects keep their data at most as long as the organization does.
  const o = useQuery(OrganizationService.method.getOrganization, {}, { staleTime: 60_000 }).data
    ?.organization;
  const orgDays = o ? o.settings?.retentionDays || o.maxRetentionDays : 0;

  return (
    <form
      className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-8 py-6"
      onSubmit={(e) => {
        e.preventDefault();
        const settings = clone(ProjectSettingsSchema, project.settings ?? create(ProjectSettingsSchema));
        settings.privacy = create(PrivacySettingsSchema, {
          ipStorage: ip,
          scrubFields: lines(scrub),
          safeFields: lines(safe),
          storeCookieValues: cookies,
          storeRequestBodies: bodies,
          discardAttachments: !attachments,
          retentionDays: Math.max(0, Math.trunc(Number(retention) || 0)),
        });
        settings.inApp = create(InAppRulesSchema, { include: lines(include), exclude: lines(exclude) });
        update.mutate({ slug: project.slug, settings });
      }}
    >
      <section className="grid grid-cols-1 gap-3">
        <div>
          <h2 className="font-semibold text-lg">İstemci IP adresi</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Olaylarla gelen IP adreslerinin nasıl saklanacağı.
          </p>
        </div>
        <RadioGroup
          value={String(ip)}
          onValueChange={(v) => setIp(Number(v) as IpStorage)}
          disabled={!admin}
          className="gap-2.5"
        >
          {ipOptions.map((o) => (
            <div key={o.value} className="flex items-start gap-2.5">
              <RadioGroupItem value={String(o.value)} id={`ip-${o.value}`} className="mt-0.5" />
              <Label htmlFor={`ip-${o.value}`} className="grid gap-0.5 font-normal">
                <span className="text-sm">{o.label}</span>
                <span className="text-xs text-muted-foreground">{o.hint}</span>
              </Label>
            </div>
          ))}
        </RadioGroup>
      </section>

      <section className="grid gap-3">
        <div>
          <h2 className="font-semibold text-lg">Saklama süresi</h2>
          <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
            Projenin olayları, ekleri ve geri bildirimleri bu kadar gün saklanır; süresi dolanlar birkaç saat
            içinde silinir.
            {orgDays
              ? ` Organizasyon verileri ${orgDays} gün saklıyor; proje daha kısa bir süre seçebilir.`
              : ""}
          </p>
        </div>
        <Field
          label="Gün"
          htmlFor="retention"
          hint="0: organizasyonun süresi. Kısaltmak kimliğinizi yeniden doğrulamanızı ister."
        >
          <Input
            id="retention"
            type="number"
            min={0}
            max={orgDays || 3650}
            value={retention}
            onChange={(e) => setRetention(e.target.value)}
            disabled={!admin}
            className="w-32"
          />
        </Field>
      </section>

      <section className="grid gap-4">
        <div>
          <h2 className="font-semibold text-lg">Hassas veriler</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Parola, token, kart numarası ve T.C. kimlik numarası gibi değerler her zaman gizlenir. Buradaki
            alanlar da gizlenir.
          </p>
        </div>
        <Field
          label="Ek olarak gizlenecek alanlar"
          htmlFor="scrub"
          hint="Her satıra bir alan adı; büyük/küçük harf fark etmez."
        >
          <Textarea
            id="scrub"
            value={scrub}
            onChange={(e) => setScrub(e.target.value)}
            disabled={!admin}
            className="min-h-20 font-mono text-sm"
            placeholder="iban\nvergi_no"
          />
        </Field>
        <Field label="Gizlenmeyecek alanlar" htmlFor="safe" hint="Yanlışlıkla gizlenen, zararsız alanlar.">
          <Textarea
            id="safe"
            value={safe}
            onChange={(e) => setSafe(e.target.value)}
            disabled={!admin}
            className="min-h-16 font-mono text-sm"
          />
        </Field>
        <div className="flex items-center gap-2.5">
          <Switch id="cookies" checked={cookies} onCheckedChange={setCookies} disabled={!admin} />
          <Label htmlFor="cookies" className="font-normal text-sm">
            Çerez değerlerini sakla
          </Label>
        </div>
        <div className="flex items-center gap-2.5">
          <Switch id="bodies" checked={bodies} onCheckedChange={setBodies} disabled={!admin} />
          <Label htmlFor="bodies" className="font-normal text-sm">
            İstek gövdelerini sakla (gizlenmiş alanlarla)
          </Label>
        </div>
        <div className="flex items-start gap-2.5">
          <Switch
            id="attachments"
            checked={attachments}
            onCheckedChange={setAttachments}
            disabled={!admin}
            className="mt-0.5"
          />
          <Label htmlFor="attachments" className="grid gap-0.5 font-normal text-sm">
            <span>Olay eklerini sakla</span>
            <span className="text-sm text-muted-foreground">
              Ekran görüntüleri ve günlük dosyaları gizleme kurallarından geçmeden saklanır. Uygulama
              göndermiyorsa bu ayarın etkisi yoktur.
            </span>
          </Label>
        </div>
      </section>

      <section className="grid gap-4">
        <div>
          <h2 className="font-semibold text-lg">Uygulama kodu</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Yığın izinde hangi çerçevelerin sizin kodunuz sayılacağı. Gruplama ve “yalnız uygulama kodu”
            görünümü buna göre çalışır.
          </p>
        </div>
        <Field
          label="Uygulama kodu sayılacak önekler"
          htmlFor="inapp-include"
          hint="Modül, paket ya da dosya yolu öneki; her satıra bir tane."
        >
          <Textarea
            id="inapp-include"
            value={include}
            onChange={(e) => setInclude(e.target.value)}
            disabled={!admin}
            className="min-h-16 font-mono text-sm"
            placeholder="KentOS.\ngithub.com/kentos/"
          />
        </Field>
        <Field label="Kütüphane sayılacak önekler" htmlFor="inapp-exclude">
          <Textarea
            id="inapp-exclude"
            value={exclude}
            onChange={(e) => setExclude(e.target.value)}
            disabled={!admin}
            className="min-h-16 font-mono text-sm"
          />
        </Field>
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
