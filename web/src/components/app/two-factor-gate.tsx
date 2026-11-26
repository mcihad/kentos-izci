import { useMutation } from "@connectrpc/connect-query";
import { Button } from "@kentos/components/ui/button";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { RecoveryCodes, TwoFactorSetup } from "@/components/account/two-factor";
import { AuthLayout } from "@/components/app/auth-layout";
import { useDocumentTitle } from "@/components/app/page";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { baseTransport } from "@/lib/api";
import { sessionQuery, useSession } from "@/lib/session";

/**
 * Shown instead of an organization that requires two-factor authentication
 * while the user has not turned it on: the setup itself, and the ways out.
 */
export function TwoFactorGate({ orgName }: { orgName: string }) {
  const session = useSession();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [codes, setCodes] = useState<string[]>();
  useDocumentTitle("İki adımlı doğrulama");
  const logout = useMutation(AuthService.method.logout, {
    transport: baseTransport,
    onSettled: () => {
      qc.clear();
      void navigate({ to: "/login" });
    },
  });
  const others = (session?.memberships ?? []).filter((m) => !m.requiresTwoFactor && m.organization);
  return (
    <AuthLayout wide>
      <div className="space-y-7">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">İki adımlı doğrulama gerekiyor</h1>
          <p className="text-md text-muted-foreground leading-relaxed">
            {orgName} organizasyonu, üyelerinin girişte parolanın yanında doğrulama uygulamasından bir kod da
            kullanmasını istiyor. Açtığınızda kaldığınız yerden devam edersiniz.
          </p>
        </div>
        {codes ? (
          <RecoveryCodes
            codes={codes}
            onDone={() => void qc.invalidateQueries({ queryKey: sessionQuery.queryKey })}
          />
        ) : (
          <TwoFactorSetup onEnabled={setCodes} />
        )}
        <div className="grid gap-3 border-t pt-5 text-sm">
          {others.length ? (
            <p className="text-muted-foreground">
              Başka organizasyona geçin:{" "}
              {others.map((m, i) => (
                <span key={m.organization?.id}>
                  {i > 0 ? ", " : null}
                  <Link
                    to="/o/$org"
                    params={{ org: m.organization?.slug ?? "" }}
                    className="font-medium text-foreground underline-offset-4 hover:underline"
                  >
                    {m.organization?.name}
                  </Link>
                </span>
              ))}
            </p>
          ) : null}
          <div>
            <Button variant="ghost" size="sm" className="-ml-2.5" onClick={() => logout.mutate({})}>
              Çıkış yap
            </Button>
          </div>
        </div>
      </div>
    </AuthLayout>
  );
}
