import { clone, create } from "@bufbuild/protobuf";
import { useState } from "react";
import { Field } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
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

  return (
    <form
      className="grid max-w-2xl grid-cols-1 gap-8 py-6"
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
        });
        settings.inApp = create(InAppRulesSchema, { include: lines(include), exclude: lines(exclude) });
        update.mutate({ slug: project.slug, settings });
      }}
    >
      <section className="grid gap-3">
        <div>
          <h2 className="font-semibold text-[15px]">İstemci IP adresi</h2>
          <p className="mt-0.5 text-[13px] text-muted-foreground">
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
                <span className="text-[13px]">{o.label}</span>
                <span className="text-[12px] text-muted-foreground">{o.hint}</span>
              </Label>
            </div>
          ))}
        </RadioGroup>
      </section>

      <section className="grid gap-4">
        <div>
          <h2 className="font-semibold text-[15px]">Hassas veriler</h2>
          <p className="mt-0.5 text-[13px] text-muted-foreground">
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
            className="min-h-20 font-mono text-[12.5px]"
            placeholder="iban\nvergi_no"
          />
        </Field>
        <Field label="Gizlenmeyecek alanlar" htmlFor="safe" hint="Yanlışlıkla gizlenen, zararsız alanlar.">
          <Textarea
            id="safe"
            value={safe}
            onChange={(e) => setSafe(e.target.value)}
            disabled={!admin}
            className="min-h-16 font-mono text-[12.5px]"
          />
        </Field>
        <div className="flex items-center gap-2.5">
          <Switch id="cookies" checked={cookies} onCheckedChange={setCookies} disabled={!admin} />
          <Label htmlFor="cookies" className="font-normal text-[13px]">
            Çerez değerlerini sakla
          </Label>
        </div>
        <div className="flex items-center gap-2.5">
          <Switch id="bodies" checked={bodies} onCheckedChange={setBodies} disabled={!admin} />
          <Label htmlFor="bodies" className="font-normal text-[13px]">
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
          <Label htmlFor="attachments" className="grid gap-0.5 font-normal text-[13px]">
            <span>Olay eklerini sakla</span>
            <span className="text-[12.5px] text-muted-foreground">
              Ekran görüntüleri ve günlük dosyaları gizleme kurallarından geçmeden saklanır. Uygulama
              göndermiyorsa bu ayarın etkisi yoktur.
            </span>
          </Label>
        </div>
      </section>

      <section className="grid gap-4">
        <div>
          <h2 className="font-semibold text-[15px]">Uygulama kodu</h2>
          <p className="mt-0.5 text-[13px] text-muted-foreground">
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
            className="min-h-16 font-mono text-[12.5px]"
            placeholder="KentOS.\ngithub.com/kentos/"
          />
        </Field>
        <Field label="Kütüphane sayılacak önekler" htmlFor="inapp-exclude">
          <Textarea
            id="inapp-exclude"
            value={exclude}
            onChange={(e) => setExclude(e.target.value)}
            disabled={!admin}
            className="min-h-16 font-mono text-[12.5px]"
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
