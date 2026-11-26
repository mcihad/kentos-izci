import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Field, FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Input } from "@kentos/components/ui/input";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { MailCheckIcon } from "lucide-react";
import { useState } from "react";
import { AuthLayout } from "@/components/app/auth-layout";
import { AuthService, SignupMode } from "@/gen/izci/api/v1/auth_pb";
import { errorField, errorMessage } from "@/lib/errors";
import { storeSession } from "@/lib/session";
import { slugify } from "@/lib/slug";

export const Route = createFileRoute("/signup")({
  component: SignupPage,
});

function SignupPage() {
  const { queryClient } = Route.useRouteContext();
  const navigate = useNavigate();
  const providers = useQuery(AuthService.method.listAuthProviders, {});
  const [form, setForm] = useState({ name: "", email: "", password: "", org: "" });
  const signup = useMutation(AuthService.method.signUp, {
    onSuccess: (res) => {
      // The account is created when the e-mailed link is opened.
      if (res.verificationSent || !res.session) return;
      storeSession(queryClient, res.session);
      const org = res.session.memberships[0]?.organization?.slug;
      void navigate({ href: org ? `/o/${org}` : "/" });
    },
  });
  const set = (patch: Partial<typeof form>) => setForm((f) => ({ ...f, ...patch }));
  const fieldError = (f: string) =>
    signup.error && errorField(signup.error) === f ? errorMessage(signup.error) : undefined;

  if (providers.data && providers.data.signupMode !== SignupMode.OPEN) {
    return (
      <AuthLayout>
        <div className="space-y-4">
          <h1 className="font-semibold text-2xl tracking-tight">Kayıt kapalı</h1>
          <p className="text-md text-muted-foreground">
            Bu sunucuya yalnızca davetle katılabilirsiniz. Organizasyonunuzun yöneticisinden davet isteyin.
          </p>
          <Button asChild variant="outline">
            <Link to="/login">Giriş sayfasına git</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }

  if (signup.data?.verificationSent) {
    return (
      <AuthLayout>
        <div className="space-y-4">
          <MailCheckIcon className="size-9 text-brand-ink" strokeWidth={1.75} />
          <h1 className="font-semibold text-2xl tracking-tight">E-postanızı kontrol edin</h1>
          <p className="text-md text-muted-foreground leading-relaxed">
            <span className="font-medium text-foreground">{form.email.trim()}</span> adresine bir bağlantı
            gönderdik. Hesabınız ve organizasyonunuz bağlantıyı açtığınızda oluşturulur. Bağlantı 24 saat
            geçerlidir.
          </p>
          <p className="text-muted-foreground text-sm leading-relaxed">
            E-posta gelmediyse istenmeyen posta klasörüne bakın ya da birkaç dakika sonra yeniden kaydolun.
          </p>
          <Button asChild variant="outline">
            <Link to="/login">Giriş sayfasına git</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <div className="space-y-7">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">Hesap açın</h1>
          <p className="text-md text-muted-foreground">
            Hesabınızla birlikte ekibiniz için bir organizasyon oluşturulur.
          </p>
        </div>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            signup.mutate({
              name: form.name,
              email: form.email,
              password: form.password,
              organizationName: form.org,
              organizationSlug: slugify(form.org),
            });
          }}
        >
          <Field label="Ad soyad" htmlFor="name" error={fieldError("name")}>
            <Input
              id="name"
              required
              autoFocus
              value={form.name}
              onChange={(e) => set({ name: e.target.value })}
            />
          </Field>
          <Field label="E-posta" htmlFor="email" error={fieldError("email")}>
            <Input
              id="email"
              type="email"
              autoComplete="username"
              required
              value={form.email}
              onChange={(e) => set({ email: e.target.value })}
            />
          </Field>
          <Field label="Parola" htmlFor="password" error={fieldError("password")} hint="En az 10 karakter.">
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              minLength={10}
              required
              value={form.password}
              onChange={(e) => set({ password: e.target.value })}
            />
          </Field>
          <Field
            label="Organizasyon adı"
            htmlFor="org"
            error={fieldError("organization_name") ?? fieldError("organization_slug")}
          >
            <Input id="org" required value={form.org} onChange={(e) => set({ org: e.target.value })} />
          </Field>
          <FormError
            message={signup.error && !errorField(signup.error) ? errorMessage(signup.error) : undefined}
          />
          <Button type="submit" className="h-10" disabled={signup.isPending}>
            {signup.isPending ? "Gönderiliyor…" : "Hesap aç"}
          </Button>
          <p className="text-md text-muted-foreground">
            Hesabınız var mı?{" "}
            <Link to="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
              Giriş yapın
            </Link>
          </p>
        </form>
      </div>
    </AuthLayout>
  );
}
