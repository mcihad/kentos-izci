import { useMutation, useQuery } from "@connectrpc/connect-query";
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
import { Input } from "@kentos/components/ui/input";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon } from "lucide-react";
import { useState } from "react";
import { CodeInput, cleanCode } from "@/components/account/two-factor";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage, errorReason } from "@/lib/errors";
import { sessionQuery, useSession } from "@/lib/session";

type Method = "password" | "code";

/**
 * Asks a signed-in user to confirm their identity before a sensitive
 * change: their password, a two-factor code or a linked identity provider.
 */
export default function ReauthDialog({ open, onClose }: { open: boolean; onClose: (ok: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={(v) => (v ? undefined : onClose(false))}>
      <DialogContent className="sm:max-w-md">
        <ReauthForm onClose={onClose} />
      </DialogContent>
    </Dialog>
  );
}

function ReauthForm({ onClose }: { onClose: (ok: boolean) => void }) {
  const user = useSession()?.user;
  const qc = useQueryClient();
  const canPassword = user?.hasPassword ?? false;
  const canCode = user?.twoFactorEnabled ?? false;
  const [method, setMethod] = useState<Method>(canPassword ? "password" : "code");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const providers = useQuery(
    AuthService.method.listAuthProviders,
    {},
    { transport: baseTransport, enabled: (user?.linkedProviders.length ?? 0) > 0 },
  );
  const linked = (providers.data?.providers ?? []).filter((p) => user?.linkedProviders.includes(p.id));
  const reauth = useMutation(AuthService.method.reauthenticate, {
    transport: baseTransport,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
      onClose(true);
    },
  });
  const form = canPassword || canCode;
  // The identity provider sends the user back to this page.
  const back = `${window.location.pathname}${window.location.search}`;
  const failure = reauth.error
    ? errorReason(reauth.error) === ErrorReason.INVALID_CREDENTIALS
      ? method === "password"
        ? "Parola hatalı."
        : "Kod hatalı ya da daha önce kullanılmış."
      : errorMessage(reauth.error)
    : undefined;
  const switchTo = (m: Method) => {
    setMethod(m);
    reauth.reset();
  };
  return (
    <>
      <DialogHeader>
        <DialogTitle>Kimliğinizi doğrulayın</DialogTitle>
        <DialogDescription>
          Bu değişiklik hesabınızın güvenliğini etkiliyor.{" "}
          {!form
            ? "Devam etmek için kimlik sağlayıcınızla yeniden giriş yapın."
            : method === "password"
              ? "Devam etmek için parolanızı girin."
              : "Devam etmek için doğrulama uygulamanızdaki kodu ya da bir kurtarma kodunu girin."}
        </DialogDescription>
      </DialogHeader>
      <form
        className="grid gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          reauth.mutate(method === "password" ? { password } : { code: code.trim() });
        }}
      >
        {form && method === "password" ? (
          <Field label="Parola" htmlFor="reauth-password">
            <Input
              id="reauth-password"
              type="password"
              autoComplete="current-password"
              required
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        ) : null}
        {form && method === "code" ? (
          <Field label="Kod" htmlFor="reauth-code">
            <CodeInput id="reauth-code" value={code} onChange={setCode} recovery autoFocus />
          </Field>
        ) : null}
        {canPassword && canCode ? (
          <div>
            <Button
              type="button"
              variant="link"
              size="sm"
              onClick={() => switchTo(method === "password" ? "code" : "password")}
            >
              {method === "password" ? "Doğrulama koduyla onayla" : "Parolayla onayla"}
            </Button>
          </div>
        ) : null}
        {linked.length ? (
          <div className="grid gap-2">
            {linked.map((p) => (
              <Button key={p.id} asChild variant="outline">
                <a href={`${p.loginUrl}?reauth=1&next=${encodeURIComponent(back)}`}>
                  <KeyRoundIcon />
                  {p.displayName} ile doğrula
                </a>
              </Button>
            ))}
            <p className="text-muted-foreground text-xs leading-relaxed">
              Kimlik sağlayıcısına gidip bu sayfaya dönersiniz; ardından işlemi yeniden başlatın.
            </p>
          </div>
        ) : null}
        {!form && !linked.length && !providers.isLoading ? (
          <p className="text-muted-foreground text-sm">
            Hesabınızı doğrulayacak bir yöntem bulunamadı. Çıkış yapıp yeniden giriş yaptıktan sonra işlemi
            tekrarlayın.
          </p>
        ) : null}
        <FormError message={failure} />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onClose(false)}>
            Vazgeç
          </Button>
          {form ? (
            <Button
              type="submit"
              disabled={
                reauth.isPending || (method === "password" ? password === "" : cleanCode(code).length < 6)
              }
            >
              {reauth.isPending ? "Doğrulanıyor…" : "Doğrula"}
            </Button>
          ) : null}
        </DialogFooter>
      </form>
    </>
  );
}
