import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Field, FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { UserService } from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { errorField, errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { recentlyAuthenticated, requestReauth } from "@/lib/reauth";
import { sessionQuery, useSession } from "@/lib/session";

export function Profile() {
  const user = useSession()?.user;
  const qc = useQueryClient();
  const [name, setName] = useState(user?.name ?? "");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const profile = useMutation(UserService.method.updateProfile, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Profil kaydedildi");
      void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const password = useMutation(AuthService.method.changePassword, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Parolanız değiştirildi. Diğer cihazlardaki oturumlar kapatıldı.");
      setCurrent("");
      setNext("");
    },
  });
  if (!user) return null;
  return (
    <div className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-10 py-6">
      <form
        className="grid gap-5"
        onSubmit={(e) => {
          e.preventDefault();
          profile.mutate({ name });
        }}
      >
        <h2 className="font-semibold text-lg">Profil</h2>
        <Field label="Ad soyad" htmlFor="me-name">
          <Input id="me-name" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="E-posta">
          <Input value={user.email} disabled />
        </Field>
        <div>
          <Button type="submit" disabled={profile.isPending}>
            Kaydet
          </Button>
        </div>
      </form>
      <SignInMethods linked={user.linkedProviders} />
      {user.hasPassword ? (
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            password.mutate({ currentPassword: current, newPassword: next });
          }}
        >
          <h2 className="font-semibold text-lg">Parola</h2>
          <Field label="Şu anki parola" htmlFor="pw-current">
            <Input
              id="pw-current"
              type="password"
              autoComplete="current-password"
              required
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </Field>
          <Field
            label="Yeni parola"
            htmlFor="pw-new"
            hint="En az 10 karakter."
            error={
              password.error && errorField(password.error) === "new_password"
                ? errorMessage(password.error)
                : undefined
            }
          >
            <Input
              id="pw-new"
              type="password"
              autoComplete="new-password"
              minLength={10}
              required
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
          </Field>
          <FormError
            message={
              password.error && errorField(password.error) !== "new_password"
                ? errorMessage(password.error)
                : undefined
            }
          />
          <div>
            <Button type="submit" disabled={password.isPending}>
              Parolayı değiştir
            </Button>
          </div>
        </form>
      ) : (
        <p className="text-sm text-muted-foreground">
          Hesabınız kimlik sağlayıcısıyla giriş yapıyor; parola tanımlı değil.
        </p>
      )}
    </div>
  );
}

/**
 * Identity providers the account can sign in with. Linking happens while
 * signed in, so that a provider never takes over an account just because
 * it reports the same e-mail address, and after a recent re-authentication.
 */
function SignInMethods({ linked }: { linked: string[] }) {
  const org = useOrg();
  const session = useSession();
  const providers = useQuery(AuthService.method.listAuthProviders, {}, { transport: baseTransport });
  const list = providers.data?.providers ?? [];
  if (list.length === 0) return null;
  const back = `/o/${org.slug}/account?tab=profile`;
  return (
    <section className="grid gap-3">
      <div>
        <h2 className="font-semibold text-lg">Giriş yöntemleri</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          Bağladığınız kimlik sağlayıcısıyla da bu hesaba giriş yaparsınız.
        </p>
      </div>
      <ul className="divide-y rounded-lg border">
        {list.map((p) => {
          const on = linked.includes(p.id);
          return (
            <li key={p.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <span className="flex min-w-0 items-center gap-2.5 text-md">
                <KeyRoundIcon className="size-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{p.displayName}</span>
              </span>
              {on ? (
                <span className="text-sm text-muted-foreground">Bağlı</span>
              ) : (
                <Button asChild variant="outline" size="sm">
                  <a
                    href={`${p.loginUrl}?link=1&next=${encodeURIComponent(back)}`}
                    onClick={(e) => {
                      if (recentlyAuthenticated(session)) return;
                      e.preventDefault();
                      const href = e.currentTarget.href;
                      void requestReauth().then((ok) => {
                        if (ok) window.location.assign(href);
                      });
                    }}
                  >
                    Bağla
                  </a>
                </Button>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
