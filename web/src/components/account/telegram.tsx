import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { SecretValue } from "@/components/common/copy-button";
import { RelativeTime } from "@/components/common/relative-time";
import { Button } from "@/components/ui/button";
import { UserService } from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { sessionQuery, useSession } from "@/lib/session";

export function Telegram() {
  const user = useSession()?.user;
  const qc = useQueryClient();
  const code = useMutation(UserService.method.createTelegramAccountLinkCode, {
    transport: baseTransport,
    onError: (err) => toast.error(errorMessage(err)),
  });
  const unlink = useMutation(UserService.method.unlinkTelegramAccount, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Telegram hesabınızın bağlantısı kaldırıldı");
      void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const tg = user?.telegram;
  return (
    <div className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-5 py-6">
      <div>
        <h2 className="font-semibold text-lg">Telegram hesabı</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          Hesabınızı bağladığınızda Telegram bildirimlerindeki “Çöz”, “Yoksay” ve “Analiz et” düğmeleri sizin
          adınıza çalışır.
        </p>
      </div>
      {tg && tg.userId !== 0n ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
          <p className="text-md">
            {tg.username ? <span className="font-medium">@{tg.username}</span> : "Telegram hesabı"} bağlı
            <span className="ml-2 text-xs text-muted-foreground">
              <RelativeTime value={tg.linkedAt} />
            </span>
          </p>
          <Button
            variant="ghost"
            size="sm"
            className="text-destructive"
            disabled={unlink.isPending}
            onClick={() => unlink.mutate({})}
          >
            Bağlantıyı kaldır
          </Button>
        </div>
      ) : code.data ? (
        <div className="grid gap-3 rounded-lg border px-4 py-4">
          <p className="text-md">
            Telegram'da <span className="font-medium">@{code.data.botUsername}</span> botuna aşağıdaki komutu
            gönderin. Kod <RelativeTime value={code.data.expiresAt} /> geçersiz olur.
          </p>
          <SecretValue value={`/ben ${code.data.code}`} />
          {code.data.link ? (
            <div>
              <Button asChild size="sm">
                <a href={code.data.link} target="_blank" rel="noreferrer">
                  Telegram'da aç
                </a>
              </Button>
            </div>
          ) : null}
        </div>
      ) : (
        <div>
          <Button onClick={() => code.mutate({})} disabled={code.isPending}>
            Telegram hesabını bağla
          </Button>
        </div>
      )}
    </div>
  );
}
