import { thClass, theadClass, trClass } from "@kentos/components/common/table";
import { Button } from "@kentos/components/ui/button";
import type * as React from "react";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import type { AuditLogEntry } from "@/gen/izci/api/v1/organization_pb";

const actionLabels: Record<string, string> = {
  "alert_rule.create": "Uyarı kuralı oluşturdu",
  "alert_rule.delete": "Uyarı kuralını sildi",
  "alert_rule.update": "Uyarı kuralını değiştirdi",
  "api_key.create": "API anahtarı oluşturdu",
  "api_key.revoke": "API anahtarını iptal etti",
  "api_key.update": "API anahtarını değiştirdi",
  "auth.login": "Giriş yaptı",
  "auth.login_failed": "Giriş denemesi başarısız",
  "auth.logout": "Çıkış yaptı",
  "auth.password_reset_request": "Parola sıfırlama bağlantısı istendi",
  "channel.create": "Bildirim kanalı ekledi",
  "channel.delete": "Bildirim kanalını kaldırdı",
  "channel.update": "Bildirim kanalını değiştirdi",
  "customer.update": "Müşteri bilgilerini değiştirdi",
  "event.delete": "Olayı sildi",
  "instance.setup": "İzci kurulumunu tamamladı",
  "invitation.create": "Üye davet etti",
  "invitation.revoke": "Daveti geri aldı",
  "issue.delete": "Hatayı sildi",
  "issue.merge": "Hataları birleştirdi",
  "issue.unmerge": "Birleştirilen hatayı ayırdı",
  "member.join": "Organizasyona katıldı",
  "member.remove": "Üyeyi çıkardı",
  "member.update": "Üyenin rolünü değiştirdi",
  "monitor.create": "Zamanlanmış görev izleyicisi oluşturdu",
  "monitor.delete": "Zamanlanmış görev izleyicisini sildi",
  "monitor.update": "Zamanlanmış görev izleyicisini değiştirdi",
  "organization.plan_update": "Planı değiştirdi",
  "organization.update": "Organizasyon ayarlarını değiştirdi",
  "privacy.delete": "Kişisel verileri sildi",
  "privacy.export": "Kişisel verileri dışa aktardı",
  "project.create": "Proje oluşturdu",
  "project.delete": "Projeyi sildi",
  "project.link_repository": "Depo bağladı",
  "project.unlink_repository": "Depo bağlantısını kaldırdı",
  "project.update": "Proje ayarlarını değiştirdi",
  "secrets.rotate": "Sırları yeni ana anahtarla yeniden mühürledi",
  "superadmin.access": "Sunucu yöneticisi olarak organizasyona girdi",
  "symbols.bundle.delete": "Kaynak eşleme paketini sildi",
  "symbols.debug_file.delete": "Sembol dosyasını sildi",
  "user.access_token.create": "Erişim anahtarı oluşturdu",
  "user.access_token.revoke": "Erişim anahtarını iptal etti",
  "user.admin_delete": "Kullanıcıyı sildi",
  "user.admin_update": "Kullanıcıyı değiştirdi",
  "user.create": "Kullanıcı oluşturdu",
  "user.delete": "Kullanıcı hesabını sildi",
  "user.password.change": "Parolasını değiştirdi",
  "user.password.reset": "Parolası sıfırlandı",
  "user.session.revoke": "Oturumlarını kapattı",
  "user.superadmin.grant": "Sunucu yöneticisi yaptı",
  "user.superadmin.revoke": "Sunucu yöneticiliğini kaldırdı",
  "user.two_factor.disable": "İki adımlı doğrulamayı kapattı",
  "user.two_factor.enable": "İki adımlı doğrulamayı açtı",
  "user.two_factor.recovery_codes": "Yeni kurtarma kodları oluşturdu",
  "user.two_factor.reset": "İki adımlı doğrulaması sıfırlandı",
};

const methods: Record<string, string> = {
  password: "parolayla",
  two_factor: "parola ve doğrulama koduyla",
  setup: "kurulumda",
  signup: "kayıt olurken",
  invitation: "daveti kabul ederken",
};

const reasons: Record<string, string> = {
  invalid_credentials: "hatalı e-posta ya da parola",
  account_disabled: "hesap devre dışı",
  too_many_attempts: "çok fazla deneme",
  second_factor: "hatalı doğrulama kodu",
};

/** The details worth showing of an entry's metadata, in Turkish. */
function details(e: AuditLogEntry): string {
  const m = e.metadata;
  const out: string[] = [];
  if (m.method) {
    out.push(
      m.method.startsWith("oidc:")
        ? `kimlik sağlayıcısıyla (${m.method.slice(5)})`
        : (methods[m.method] ?? m.method),
    );
  }
  if (m.device) out.push(m.device);
  if (m.reason) out.push(reasons[m.reason] ?? m.reason);
  if (m.via === "cli")
    out.push(`komut satırından${m.os_user ? ` (${m.os_user}${m.host ? `@${m.host}` : ""})` : ""}`);
  return out.join(", ");
}

/** Audit log entries as a table (a list on phones), with paging. */
export function AuditEntries({
  rows,
  hasMore,
  onMore,
  empty,
}: {
  rows: AuditLogEntry[];
  hasMore: boolean;
  onMore: () => void;
  empty: React.ReactNode;
}) {
  if (rows.length === 0)
    return (
      <EmptyState title="Henüz kayıt yok" className="my-6 max-w-5xl rounded-lg border py-10">
        {empty}
      </EmptyState>
    );
  return (
    <div className="max-w-5xl py-6">
      <ul className="divide-y rounded-lg border md:hidden">
        {rows.map((e) => (
          <li key={e.id} className="px-4 py-3">
            <div className="flex items-baseline justify-between gap-3">
              <p className="font-medium text-md">{actionLabels[e.action] ?? e.action}</p>
              <span className="shrink-0 text-sm text-muted-foreground">
                <RelativeTime value={e.createdAt} />
              </span>
            </div>
            <p className="mt-0.5 truncate text-sm text-muted-foreground">
              {e.actor?.name || e.actor?.email || "Sistem"}
              {e.ipAddress ? `, ${e.ipAddress}` : ""}
            </p>
            {e.targetName || e.targetId ? (
              <p className="mt-1 truncate font-mono text-sm">{e.targetName || e.targetId}</p>
            ) : null}
            {details(e) ? <p className="mt-0.5 text-muted-foreground text-xs">{details(e)}</p> : null}
          </li>
        ))}
      </ul>
      <div className="hidden overflow-x-auto rounded-lg border md:block">
        <table className="w-full text-sm">
          <thead className={theadClass}>
            <tr>
              <th className={thClass}>Zaman</th>
              <th className={thClass}>Kişi</th>
              <th className={thClass}>İşlem</th>
              <th className={thClass}>Hedef</th>
              <th className={thClass}>IP</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {rows.map((e) => (
              <tr key={e.id} className={trClass}>
                <td className="whitespace-nowrap px-3.5 py-2 text-muted-foreground">
                  <RelativeTime value={e.createdAt} />
                </td>
                <td className="max-w-44 truncate px-3.5 py-2">
                  {e.actor?.name || e.actor?.email || "Sistem"}
                </td>
                <td className="px-3.5 py-2">
                  {actionLabels[e.action] ?? e.action}
                  {details(e) ? (
                    <span className="block text-muted-foreground text-xs">{details(e)}</span>
                  ) : null}
                </td>
                <td className="max-w-52 truncate px-3.5 py-2 font-mono text-sm text-muted-foreground">
                  {e.targetName || e.targetId}
                </td>
                <td className="px-3.5 py-2 font-mono text-sm text-muted-foreground">{e.ipAddress || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {hasMore ? (
        <Button variant="ghost" size="sm" className="mt-2" onClick={onMore}>
          Daha eski kayıtlar
        </Button>
      ) : null}
    </div>
  );
}
