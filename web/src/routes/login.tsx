import { createQueryOptions, useMutation, useQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link, redirect, useNavigate } from "@tanstack/react-router";
import { KeyRoundIcon } from "lucide-react";
import { useState } from "react";
import { AuthLayout } from "@/components/app/auth-layout";
import { Field, FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AuthService, SignupMode } from "@/gen/izci/api/v1/auth_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage, oidcErrors } from "@/lib/errors";
import { safeRedirect } from "@/lib/redirect";
import { firstOrganization, loadSession, storeSession } from "@/lib/session";

type LoginSearch = { redirect?: string; error?: string };

export const Route = createFileRoute("/login")({
  validateSearch: (s: Record<string, unknown>): LoginSearch => ({
    redirect: typeof s.redirect === "string" ? s.redirect : undefined,
    error: typeof s.error === "string" ? s.error : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const session = await loadSession(context.queryClient);
    if (session) {
      const org = firstOrganization(session);
      throw redirect({ href: safeRedirect(search.redirect) ?? (org ? `/o/${org}` : "/") });
    }
    const setup = await context.queryClient.ensureQueryData(
      createQueryOptions(AuthService.method.getSetupState, {}, { transport: baseTransport }),
    );
    if (setup.setupRequired) throw redirect({ to: "/setup" });
  },
  component: LoginPage,
});

function LoginPage() {
  const search = Route.useSearch();
  const navigate = useNavigate();
  const { queryClient } = Route.useRouteContext();
  const providers = useQuery(AuthService.method.listAuthProviders, {});
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const login = useMutation(AuthService.method.login, {
    onSuccess: (res) => {
      storeSession(queryClient, res.session);
      const org = firstOrganization(res.session);
      void navigate({ href: safeRedirect(search.redirect) ?? (org ? `/o/${org}` : "/") });
    },
  });

  const passwordLogin = providers.data?.passwordLoginEnabled ?? true;
  const oidc = providers.data?.providers ?? [];
  const next = safeRedirect(search.redirect);

  return (
    <AuthLayout>
      <div className="space-y-7">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">Giriş yap</h1>
          <p className="text-muted-foreground text-sm">Hataları görmek için hesabınızla oturum açın.</p>
        </div>

        <FormError
          message={
            search.error ? (oidcErrors[search.error] ?? "Giriş yapılamadı. Yeniden deneyin.") : undefined
          }
        />

        {oidc.length > 0 ? (
          <div className="grid gap-2">
            {oidc.map((p) => (
              <Button key={p.id} asChild variant="outline" className="h-10 justify-start gap-3">
                <a href={`${p.loginUrl}${next ? `?next=${encodeURIComponent(next)}` : ""}`}>
                  <KeyRoundIcon className="size-4 text-muted-foreground" />
                  {p.displayName} ile devam et
                </a>
              </Button>
            ))}
          </div>
        ) : null}

        {oidc.length > 0 && passwordLogin ? (
          <div className="flex items-center gap-3 text-muted-foreground text-xs">
            <span className="h-px flex-1 bg-border" />
            ya da e-posta ile
            <span className="h-px flex-1 bg-border" />
          </div>
        ) : null}

        {passwordLogin ? (
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              login.mutate({ email, password });
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
            <Field label="Parola" htmlFor="password">
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            <FormError message={login.error ? errorMessage(login.error) : undefined} />
            <Button type="submit" className="h-10" disabled={login.isPending}>
              {login.isPending ? "Giriş yapılıyor…" : "Giriş yap"}
            </Button>
          </form>
        ) : null}

        {providers.data?.signupMode === SignupMode.OPEN ? (
          <p className="text-muted-foreground text-sm">
            Hesabınız yok mu?{" "}
            <Link to="/signup" className="font-medium text-foreground underline-offset-4 hover:underline">
              Kayıt olun
            </Link>
          </p>
        ) : null}
      </div>
    </AuthLayout>
  );
}
