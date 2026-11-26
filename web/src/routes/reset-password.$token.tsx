import { useMutation } from "@connectrpc/connect-query";
import { Field, FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Input } from "@kentos/components/ui/input";
import { createFileRoute, Link } from "@tanstack/react-router";
import { CircleCheckIcon } from "lucide-react";
import { useState } from "react";
import { AuthLayout } from "@/components/app/auth-layout";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { errorField, errorMessage, errorReason } from "@/lib/errors";

export const Route = createFileRoute("/reset-password/$token")({
  component: ResetPasswordPage,
});

/** Sets a new password from an e-mailed link. */
function ResetPasswordPage() {
  const { token } = Route.useParams();
  const [password, setPassword] = useState("");
  const reset = useMutation(AuthService.method.resetPassword);
  const invalid = reset.error && errorReason(reset.error) === ErrorReason.VERIFICATION_LINK_INVALID;

  if (reset.isSuccess) {
    return (
      <AuthLayout>
        <div className="space-y-4">
          <CircleCheckIcon className="size-9 text-brand-ink" strokeWidth={1.75} />
          <h1 className="font-semibold text-2xl tracking-tight">Parolanız değiştirildi</h1>
          <p className="text-md text-muted-foreground leading-relaxed">
            Bütün cihazlardaki oturumlar kapatıldı. Yeni parolanızla giriş yapın.
          </p>
          <Button asChild>
            <Link to="/login">Giriş yap</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }
  if (invalid) {
    return (
      <AuthLayout>
        <div className="space-y-4">
          <h1 className="font-semibold text-2xl tracking-tight">Bağlantı geçersiz</h1>
          <p className="text-md text-muted-foreground leading-relaxed">
            Bağlantı daha önce kullanılmış ya da 1 saatlik süresi dolmuş. Yeni bir bağlantı isteyin.
          </p>
          <Button asChild variant="outline">
            <Link to="/forgot-password">Yeni bağlantı iste</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }

  const fieldError =
    reset.error && errorField(reset.error) === "new_password" ? errorMessage(reset.error) : undefined;
  return (
    <AuthLayout>
      <div className="space-y-7">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">Yeni parola belirleyin</h1>
          <p className="text-md text-muted-foreground">
            Parolanız değişince bütün cihazlardaki oturumlar kapatılır.
          </p>
        </div>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            reset.mutate({ token, newPassword: password });
          }}
        >
          <Field label="Yeni parola" htmlFor="password" hint="En az 10 karakter." error={fieldError}>
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              minLength={10}
              required
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
          <FormError message={reset.error && !fieldError ? errorMessage(reset.error) : undefined} />
          <Button type="submit" className="h-10" disabled={reset.isPending}>
            {reset.isPending ? "Kaydediliyor…" : "Parolayı değiştir"}
          </Button>
        </form>
      </div>
    </AuthLayout>
  );
}
