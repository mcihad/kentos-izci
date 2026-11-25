import { createQueryOptions, useMutation, useQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link, redirect, useNavigate } from "@tanstack/react-router";
import { KeyRoundIcon } from "lucide-react";
import { useState } from "react";
import { CodeInput, cleanCode } from "@/components/account/two-factor";
import { AuthLayout } from "@/components/app/auth-layout";
import { Field, FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AuthService, type Session, SignupMode } from "@/gen/izci/api/v1/auth_pb";
import { ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage, errorReason, oidcErrors } from "@/lib/errors";
import { safeRedirect } from "@/lib/redirect";
import { firstOrganization, loadSession, storeSession } from "@/lib/session";

// two_factor: an OpenID Connect sign-in waits for its second step (the
// server redirects with two_factor=1).
type LoginSearch = { redirect?: string; error?: string; two_factor?: boolean };

export const Route = createFileRoute("/login")({
  validateSearch: (s: Record<string, unknown>): LoginSearch => ({
    redirect: typeof s.redirect === "string" ? s.redirect : undefined,
    error: typeof s.error === "string" ? s.error : undefined,
    two_factor: s.two_factor === 1 || s.two_factor === "1" || s.two_factor === true ? true : undefined,
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
  const [secondStep, setSecondStep] = useState(search.two_factor === true);
  const [expired, setExpired] = useState(false);
  const signedIn = (session: Session | undefined) => {
    storeSession(queryClient, session);
    const org = firstOrganization(session);
    void navigate({ href: safeRedirect(search.redirect) ?? (org ? `/o/${org}` : "/") });
  };
  const login = useMutation(AuthService.method.login, {
    onSuccess: (res) => {
      if (res.twoFactorRequired) {
        setExpired(false);
        setSecondStep(true);
        return;
      }
      signedIn(res.session);
    },
  });

  const passwordLogin = providers.data?.passwordLoginEnabled ?? true;
  const oidc = providers.data?.providers ?? [];
  const next = safeRedirect(search.redirect);

  if (secondStep) {
    return (
      <AuthLayout>
        <SecondStep
          onSignedIn={signedIn}
          onRestart={(timedOut) => {
            setExpired(timedOut);
            setSecondStep(false);
            setPassword("");
            login.reset();
            if (search.two_factor) void navigate({ to: "/login", search: { redirect: search.redirect } });
          }}
        />
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <div className="space-y-7">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">Giriş yap</h1>
          <p className="text-md text-muted-foreground">Hataları görmek için hesabınızla oturum açın.</p>
        </div>

        <FormError
          message={
            expired
              ? "Girişin süresi doldu. Yeniden giriş yapın."
              : search.error
                ? (oidcErrors[search.error] ?? "Giriş yapılamadı. Yeniden deneyin.")
                : undefined
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
          <div className="flex items-center gap-3 text-muted-foreground text-sm">
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
            <Field
              label="Parola"
              htmlFor="password"
              hint={
                providers.data?.passwordResetAvailable ? (
                  <Link
                    to="/forgot-password"
                    className="underline-offset-4 hover:text-foreground hover:underline"
                  >
                    Parolamı unuttum
                  </Link>
                ) : undefined
              }
            >
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
          <p className="text-md text-muted-foreground">
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

/**
 * The second step of a sign-in: a code from the authenticator app, or a
 * recovery code. Six typed digits submit on their own.
 */
function SecondStep({
  onSignedIn,
  onRestart,
}: {
  onSignedIn: (session: Session | undefined) => void;
  onRestart: (expired: boolean) => void;
}) {
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  const verify = useMutation(AuthService.method.verifyTwoFactor, {
    onSuccess: (res) => onSignedIn(res.session),
    onError: (err) => {
      if (errorReason(err) === ErrorReason.TWO_FACTOR_EXPIRED) onRestart(true);
    },
  });
  const submit = (value: string) => {
    if (!verify.isPending) verify.mutate({ code: recovery ? value.trim() : cleanCode(value) });
  };
  return (
    <div className="space-y-7">
      <div className="space-y-1.5">
        <h1 className="font-semibold text-2xl tracking-tight">İki adımlı doğrulama</h1>
        <p className="text-md text-muted-foreground">
          {recovery
            ? "Kurtarma kodlarınızdan birini girin. Her kod bir kez kullanılabilir."
            : "Doğrulama uygulamanızın gösterdiği 6 haneli kodu girin."}
        </p>
      </div>
      <form
        className="grid gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit(code);
        }}
      >
        <Field label={recovery ? "Kurtarma kodu" : "Doğrulama kodu"} htmlFor="login-code">
          <CodeInput
            id="login-code"
            key={recovery ? "recovery" : "totp"}
            value={code}
            recovery={recovery}
            autoFocus
            onChange={(v) => {
              setCode(v);
              if (!recovery && cleanCode(v).length === 6 && cleanCode(code).length !== 6) submit(v);
            }}
          />
        </Field>
        <FormError message={verify.error ? errorMessage(verify.error) : undefined} />
        <Button type="submit" className="h-10" disabled={verify.isPending}>
          {verify.isPending ? "Doğrulanıyor…" : "Doğrula"}
        </Button>
      </form>
      <div className="flex flex-wrap justify-between gap-x-4 gap-y-2 text-md">
        <button
          type="button"
          className="font-medium underline-offset-4 hover:underline"
          onClick={() => {
            setRecovery(!recovery);
            setCode("");
            verify.reset();
          }}
        >
          {recovery ? "Doğrulama uygulamasını kullan" : "Kurtarma kodu kullan"}
        </button>
        <button
          type="button"
          className="text-muted-foreground underline-offset-4 hover:underline"
          onClick={() => onRestart(false)}
        >
          Başka hesapla giriş yap
        </button>
      </div>
    </div>
  );
}
