import { clone, create } from "@bufbuild/protobuf";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  DigestFrequency,
  type NotificationChannels,
  NotificationChannelsSchema,
  type NotificationSettings,
  NotificationSettingsSchema,
  UserService,
} from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage } from "@/lib/errors";

type Kind = "assigned" | "mentioned" | "regressed" | "commented";

const kinds: { key: Kind; title: string; detail: string }[] = [
  {
    key: "assigned",
    title: "Bir hata bana atandığında",
    detail: "Bir üye ya da bir sahiplik kuralı atadığında.",
  },
  {
    key: "mentioned",
    title: "Bir yorumda anıldığımda",
    detail: "Yorumda @ ile e-posta adresinizin @ öncesi ya da tamamı yazıldığında.",
  },
  {
    key: "regressed",
    title: "Takip ettiğim hata geri döndüğünde",
    detail: "Çözülen hata yeniden görüldüğünde.",
  },
  { key: "commented", title: "Takip ettiğim hataya yorum yazıldığında", detail: "Kendi yorumlarınız hariç." },
];

/** Account tab: where personal notifications go besides the inbox. */
export function NotificationPreferences() {
  const q = useQuery(UserService.method.getNotificationSettings, {}, { transport: baseTransport });
  if (!q.data?.settings) return <Skeleton className="my-6 h-64 w-full max-w-2xl" />;
  return (
    <PreferencesForm
      initial={q.data.settings}
      emailAvailable={q.data.emailAvailable}
      telegramAvailable={q.data.telegramAvailable}
    />
  );
}

function PreferencesForm({
  initial,
  emailAvailable,
  telegramAvailable,
}: {
  initial: NotificationSettings;
  emailAvailable: boolean;
  telegramAvailable: boolean;
}) {
  const qc = useQueryClient();
  const [settings, setSettings] = useState(() => clone(NotificationSettingsSchema, initial));
  const save = useMutation(UserService.method.updateNotificationSettings, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Bildirim tercihleri kaydedildi");
      void qc.invalidateQueries();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const channelsOf = (k: Kind): NotificationChannels => settings[k] ?? create(NotificationChannelsSchema);
  const set = (k: Kind, field: "email" | "telegram", on: boolean) => {
    const next = clone(NotificationSettingsSchema, settings);
    next[k] = create(NotificationChannelsSchema, { ...channelsOf(k), [field]: on });
    setSettings(next);
  };

  return (
    <form
      className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-5 py-6"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate({ settings });
      }}
    >
      <div>
        <h2 className="font-semibold text-lg">Bildirimler</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          Bildirimlerin hepsi üst çubuktaki bildirim kutusuna düşer; burada hangilerinin ayrıca e-postayla ya
          da Telegram'la gelmesini seçersiniz. Size atanan, yorum yazdığınız ve anıldığınız hataları
          kendiliğinden takip edersiniz; hata sayfasındaki{" "}
          <b className="font-medium text-foreground">Takip et</b> ile dilediğiniz hatayı da takip
          edebilirsiniz.
        </p>
      </div>
      <div className="overflow-hidden rounded-lg border">
        <table className="w-full text-sm">
          <thead className="bg-(--surface-muted) text-muted-foreground text-xs">
            <tr>
              <th className="px-4 py-2 text-left font-medium">Ne zaman</th>
              <th className="w-24 px-2 py-2 text-center font-medium">E-posta</th>
              <th className="w-24 px-2 py-2 text-center font-medium">Telegram</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {kinds.map((k) => (
              <tr key={k.key}>
                <td className="px-4 py-3">
                  <p className="font-medium">{k.title}</p>
                  <p className="text-muted-foreground text-xs">{k.detail}</p>
                </td>
                <td className="px-2 py-3 text-center">
                  <Switch
                    checked={channelsOf(k.key).email}
                    disabled={!emailAvailable}
                    onCheckedChange={(v) => set(k.key, "email", v)}
                    aria-label={`${k.title}: e-posta`}
                  />
                </td>
                <td className="px-2 py-3 text-center">
                  <Switch
                    checked={channelsOf(k.key).telegram}
                    disabled={!telegramAvailable}
                    onCheckedChange={(v) => set(k.key, "telegram", v)}
                    aria-label={`${k.title}: Telegram`}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Field
        label="Özet e-postası"
        htmlFor="digest"
        hint="Üyesi olduğunuz organizasyonların olay sayılarını, yeni, geri dönen ve en çok artan hatalarını özetler. Günlük özet her sabah 08.00'de son 24 saati, haftalık özet pazartesi sabahı geçen haftayı kapsar."
      >
        <Select
          value={String(settings.digest)}
          disabled={!emailAvailable}
          onValueChange={(v) => {
            const next = clone(NotificationSettingsSchema, settings);
            next.digest = Number(v) as DigestFrequency;
            setSettings(next);
          }}
        >
          <SelectTrigger id="digest" className="w-full sm:w-64">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={String(DigestFrequency.UNSPECIFIED)}>Gönderilmesin</SelectItem>
            <SelectItem value={String(DigestFrequency.DAILY)}>Her gün</SelectItem>
            <SelectItem value={String(DigestFrequency.WEEKLY)}>Her hafta</SelectItem>
          </SelectContent>
        </Select>
      </Field>
      {!emailAvailable || !telegramAvailable ? (
        <ul className="list-disc space-y-1 pl-5 text-muted-foreground text-sm marker:text-muted-foreground">
          {!emailAvailable ? (
            <li>E-posta için sunucu yöneticisinin SMTP ayarlarını yapması gerekir.</li>
          ) : null}
          {!telegramAvailable ? (
            <li>Telegram için Telegram sekmesinden hesabınızı bota bağlayın (sunucuda bot ayarlıysa).</li>
          ) : null}
        </ul>
      ) : null}
      <div>
        <Button type="submit" disabled={save.isPending}>
          Kaydet
        </Button>
      </div>
    </form>
  );
}
