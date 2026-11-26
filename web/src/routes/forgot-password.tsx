import { useMutation } from "@connectrpc/connect-query";
import { Field, FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Input } from "@kentos/components/ui/input";
import { createFileRoute, Link } from "@tanstack/react-router";
import { MailCheckIcon } from "lucide-react";
import { useState } from "react";
import { AuthLayout } from "@/components/app/auth-layout";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { errorMessage } from "@/lib/errors";

export const Route = createFileRoute("/forgot-password")({
  component: ForgotPasswordPage,
});

/** Asks for a link that sets a new password. */
function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const request = useMutation(AuthService.method.requestPasswordReset);

  if (request.isSuccess) {
    return (
      <AuthLayout>
        <div className="space-y-4">
          <MailCheckIcon className="size-9 text-brand-ink" strokeWidth={1.75} />
          <h1 className="font-semibold text-2xl tracking-tight">E-postanızı kontrol edin</h1>
          <p className="text-md text-muted-foreground leading-relaxed">
            <span className="font-medium text-foreground">{email.trim()}</span> adresiyle açılmış bir hesap
            varsa yeni parola belirleme bağlantısı gönderildi. Bağlantı 1 saat geçerlidir.
          </p>
          <p className="text-muted-foreground text-sm leading-relaxed">
            E-posta gelmediyse istenmeyen posta klasörüne bakın ya da birkaç dakika sonra yeniden deneyin.
          </p>
          <Button asChild variant="outline">
            <Link to="/login">Giriş sayfasına dön</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <div className="space-y-7">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">Parolanızı mı unuttunuz?</h1>
          <p className="text-md text-muted-foreground">
            Hesabınızın e-posta adresini yazın; yeni parola belirlemeniz için bir bağlantı gönderelim.
          </p>
        </div>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            request.mutate({ email: email.trim() });
          }}
        >
          <Field label="E-posta" htmlFor="email">
            <Input
              id="email"
              type="email"
              autoComplete="username"
              required
              autoFocus
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </Field>
          <FormError message={request.error ? errorMessage(request.error) : undefined} />
          <Button type="submit" className="h-10" disabled={request.isPending}>
            {request.isPending ? "Gönderiliyor…" : "Bağlantı gönder"}
          </Button>
          <p className="text-md text-muted-foreground">
            <Link to="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
              Giriş sayfasına dön
            </Link>
          </p>
        </form>
      </div>
    </AuthLayout>
  );
}
