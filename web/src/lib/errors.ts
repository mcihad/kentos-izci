import { Code, ConnectError } from "@connectrpc/connect";
import { type ErrorDetail, ErrorDetailSchema, ErrorReason } from "@/gen/izci/api/v1/common_pb";

/** Turkish messages for machine-readable API error reasons. */
const reasonMessages: Partial<Record<ErrorReason, string>> = {
  [ErrorReason.UNAUTHENTICATED]: "Oturumunuz sona erdi. Yeniden giriş yapın.",
  [ErrorReason.PERMISSION_DENIED]: "Bu işlem için yetkiniz yok.",
  [ErrorReason.ORGANIZATION_REQUIRED]: "Bu organizasyona erişiminiz yok.",
  [ErrorReason.NOT_FOUND]: "Kayıt bulunamadı. Silinmiş ya da taşınmış olabilir.",
  [ErrorReason.INVALID_ARGUMENT]: "Girilen bilgileri kontrol edin.",
  [ErrorReason.INVALID_CREDENTIALS]: "E-posta adresi ya da parola hatalı.",
  [ErrorReason.ACCOUNT_DISABLED]: "Hesabınız devre dışı. Bir yöneticiyle görüşün.",
  [ErrorReason.TOO_MANY_ATTEMPTS]: "Çok fazla deneme yapıldı. Birkaç dakika sonra yeniden deneyin.",
  [ErrorReason.WEAK_PASSWORD]: "Parola en az 10 karakter olmalı ve kolay tahmin edilmemeli.",
  [ErrorReason.SETUP_COMPLETED]: "Kurulum zaten tamamlanmış. Giriş yapabilirsiniz.",
  [ErrorReason.SETUP_REQUIRED]: "Önce kurulumu tamamlayın.",
  [ErrorReason.SIGNUP_DISABLED]: "Kayıt kapalı. Bir yöneticiden davet isteyin.",
  [ErrorReason.EMAIL_TAKEN]: "Bu e-posta adresiyle açılmış bir hesap var.",
  [ErrorReason.SLUG_TAKEN]: "Bu kısa ad kullanılıyor. Başka bir kısa ad seçin.",
  [ErrorReason.INVITATION_INVALID]: "Davet bağlantısı geçersiz ya da süresi dolmuş.",
  [ErrorReason.LAST_OWNER]: "Organizasyonun en az bir sahibi kalmalı.",
  [ErrorReason.LIMIT_REACHED]: "Planınızın sınırına ulaştınız.",
  [ErrorReason.QUOTA_EXCEEDED]: "Bu ayın olay kotası doldu.",
  [ErrorReason.INVALID_QUERY]: "Arama ifadesi anlaşılamadı.",
  [ErrorReason.INTEGRATION_FAILED]: "Entegrasyona ulaşılamadı. Bağlantı ayarlarını kontrol edin.",
  [ErrorReason.GITHUB_TOKEN_INVALID]: "GitHub erişim anahtarı geçersiz ya da yetkisi yetersiz.",
  [ErrorReason.GITHUB_APP_NOT_CONFIGURED]: "GitHub App bu sunucuda yapılandırılmamış.",
  [ErrorReason.REPOSITORY_NOT_LINKED]: "Bu projeye bağlı bir depo yok.",
  [ErrorReason.AI_DISABLED]: "Yapay zekâ analizi bu sunucuda kapalı.",
  [ErrorReason.AI_BUDGET_EXCEEDED]: "Günlük yapay zekâ bütçesi doldu.",
  [ErrorReason.AI_RUN_ACTIVE]: "Bu hata için süren bir analiz var.",
  [ErrorReason.AI_RUN_NOT_READY]: "Analiz henüz tamamlanmadı.",
  [ErrorReason.TELEGRAM_NOT_CONFIGURED]: "Telegram botu bu sunucuda yapılandırılmamış.",
  [ErrorReason.CONFLICT]: "İşlem mevcut durumla çakışıyor. Sayfayı yenileyip yeniden deneyin.",
  [ErrorReason.INTERNAL]: "Sunucuda beklenmeyen bir hata oluştu. Yeniden deneyin.",
};

/** The ErrorDetail attached to an API error, if any. */
export function errorDetail(err: unknown): ErrorDetail | undefined {
  if (!(err instanceof ConnectError)) return undefined;
  return err.findDetails(ErrorDetailSchema)[0];
}

/** The error reason of an API error (UNSPECIFIED when unknown). */
export function errorReason(err: unknown): ErrorReason {
  return errorDetail(err)?.reason ?? ErrorReason.UNSPECIFIED;
}

/** A user-facing Turkish message for any error thrown by an API call. */
export function errorMessage(err: unknown): string {
  const reason = errorReason(err);
  const known = reasonMessages[reason];
  if (known) return known;
  if (err instanceof ConnectError) {
    switch (err.code) {
      case Code.Unavailable:
        return "Sunucuya ulaşılamıyor. Bağlantınızı kontrol edin.";
      case Code.DeadlineExceeded:
        return "Sunucu zamanında yanıt vermedi. Yeniden deneyin.";
      case Code.Unimplemented:
        return "Bu özellik bu sunucuda henüz yok.";
      case Code.ResourceExhausted:
        return "İstek sınırına ulaşıldı. Biraz bekleyip yeniden deneyin.";
      case Code.InvalidArgument:
        return "Girilen bilgileri kontrol edin.";
      case Code.PermissionDenied:
        return "Bu işlem için yetkiniz yok.";
      case Code.NotFound:
        return "Kayıt bulunamadı.";
    }
  }
  return "Beklenmeyen bir hata oluştu. Yeniden deneyin.";
}

/** The request field an INVALID_ARGUMENT error refers to, if any. */
export function errorField(err: unknown): string {
  return errorDetail(err)?.field ?? "";
}

/** True when the error means the resource does not exist. */
export function isNotFound(err: unknown): boolean {
  return err instanceof ConnectError && err.code === Code.NotFound;
}

/** Messages for the error codes the OIDC callback puts into /login?error=. */
export const oidcErrors: Record<string, string> = {
  oidc_denied: "Kimlik sağlayıcısında giriş iptal edildi.",
  oidc_domain: "E-posta alan adınız bu sunucuda giriş için izinli değil.",
  oidc_email: "Kimlik sağlayıcısı doğrulanmış bir e-posta adresi göndermedi.",
  oidc_exchange: "Kimlik sağlayıcısıyla oturum açılamadı. Yeniden deneyin.",
  oidc_internal: "Giriş sırasında beklenmeyen bir hata oluştu. Yeniden deneyin.",
  oidc_no_account: "Bu e-posta adresiyle bir İzci hesabı yok. Bir yöneticiden davet isteyin.",
  oidc_state: "Giriş isteğinin süresi doldu. Yeniden deneyin.",
  oidc_token: "Kimlik sağlayıcısının yanıtı doğrulanamadı.",
  oidc_unavailable: "Kimlik sağlayıcısına şu anda ulaşılamıyor.",
};
