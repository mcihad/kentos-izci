import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Field, FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Input } from "@kentos/components/ui/input";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { AuthLayout } from "@/components/app/auth-layout";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { errorField, errorMessage } from "@/lib/errors";
import { roleLabels } from "@/lib/labels";
import { storeSession, useSession } from "@/lib/session";

export const Route = createFileRoute("/invite/$token")({
  component: InvitePage,
});

function InvitePage() {
  const { token } = Route.useParams();
  const { queryClient } = Route.useRouteContext();
  const navigate = useNavigate();
  const session = useSession();
  const info = useQuery(AuthService.method.getInvitation, { token }, { retry: false });
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const accept = useMutation(AuthService.method.acceptInvitation, {
    onSuccess: (res) => {
      storeSession(queryClient, res.session);
      const org = info.data?.invitation?.organization?.slug;
      void navigate({ href: org ? `/o/${org}` : "/" });
    },
  });

  if (info.isPending) {
    return (
      <AuthLayout>
        <div className="space-y-3">
          <Skeleton className="h-7 w-2/3" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-4/5" />
        </div>
      </AuthLayout>
    );
  }
  if (info.error || !info.data?.invitation) {
    return (
      <AuthLayout>
        <div className="space-y-4">
          <h1 className="font-semibold text-2xl tracking-tight">Davet açılamadı</h1>
          <p className="text-md text-muted-foreground">{errorMessage(info.error)}</p>
          <Button asChild variant="outline">
            <Link to="/login">Giriş sayfasına git</Link>
          </Button>
        </div>
      </AuthLayout>
    );
  }

  const inv = info.data.invitation;
  const org = inv.organization?.name ?? "";
  const signedInAs = session?.user?.email;
  const needsLogin = info.data.accountExists && !signedInAs;

  return (
    <AuthLayout>
      <div className="space-y-6">
        <div className="space-y-2">
          <h1 className="font-semibold text-2xl tracking-tight">{org} sizi bekliyor</h1>
          <p className="text-muted-foreground text-sm leading-relaxed">
            {inv.invitedBy?.name || inv.invitedBy?.email || "Bir yönetici"}, sizi{" "}
            <strong className="font-medium text-foreground">{org}</strong> organizasyonuna{" "}
            {roleLabels[inv.role].toLocaleLowerCase("tr-TR")} olarak davet etti. Davet{" "}
            <span className="text-foreground">{inv.email}</span> adresine gönderildi.
          </p>
        </div>

        {needsLogin ? (
          <div className="grid gap-3">
            <p className="text-sm">
              Bu e-posta adresiyle bir hesabınız var. Daveti kabul etmek için giriş yapın.
            </p>
            <Button asChild>
              <Link to="/login" search={{ redirect: `/invite/${token}` }}>
                Giriş yap
              </Link>
            </Button>
          </div>
        ) : signedInAs ? (
          <div className="grid gap-3">
            <p className="text-md text-muted-foreground">
              <span className="text-foreground">{signedInAs}</span> olarak oturum açtınız.
            </p>
            <FormError message={accept.error ? errorMessage(accept.error) : undefined} />
            <Button disabled={accept.isPending} onClick={() => accept.mutate({ token })}>
              {accept.isPending ? "Katılınıyor…" : `${org} organizasyonuna katıl`}
            </Button>
          </div>
        ) : (
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              accept.mutate({ token, name, password });
            }}
          >
            <Field label="Ad soyad" htmlFor="name">
              <Input id="name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field
              label="Parola"
              htmlFor="password"
              error={
                accept.error && errorField(accept.error) === "password"
                  ? errorMessage(accept.error)
                  : undefined
              }
              hint="En az 10 karakter."
            >
              <Input
                id="password"
                type="password"
                autoComplete="new-password"
                minLength={10}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            <FormError
              message={
                accept.error && errorField(accept.error) !== "password"
                  ? errorMessage(accept.error)
                  : undefined
              }
            />
            <Button type="submit" disabled={accept.isPending}>
              {accept.isPending ? "Hesap açılıyor…" : "Hesabı aç ve katıl"}
            </Button>
          </form>
        )}
      </div>
    </AuthLayout>
  );
}
