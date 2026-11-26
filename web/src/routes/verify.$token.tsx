import { useMutation } from "@connectrpc/connect-query";
import { FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { AuthLayout } from "@/components/app/auth-layout";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { errorMessage, errorReason } from "@/lib/errors";
import { storeSession } from "@/lib/session";

export const Route = createFileRoute("/verify/$token")({
  component: VerifyPage,
});

/**
 * Finishes a self sign-up from the e-mailed link. The account is created
 * on a button press, not on page load: mail scanners that open links must
 * not use up the link.
 */
function VerifyPage() {
  const { token } = Route.useParams();
  const { queryClient } = Route.useRouteContext();
  const navigate = useNavigate();
  const verify = useMutation(AuthService.method.verifySignup, {
    onSuccess: (res) => {
      storeSession(queryClient, res.session);
      const org = res.session?.memberships[0]?.organization?.slug;
      void navigate({ href: org ? `/o/${org}` : "/" });
    },
  });
  const invalid = verify.error && errorReason(verify.error) === ErrorReason.VERIFICATION_LINK_INVALID;

  if (invalid) {
    return (
      <AuthLayout>
        <div className="space-y-4">
          <h1 className="font-semibold text-2xl tracking-tight">Bağlantı geçersiz</h1>
          <p className="text-md text-muted-foreground leading-relaxed">
            Bağlantı daha önce kullanılmış ya da 24 saatlik süresi dolmuş. Hesabınız açıldıysa giriş yapın;
            açılmadıysa yeniden kaydolun.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button asChild>
              <Link to="/login">Giriş yap</Link>
            </Button>
            <Button asChild variant="outline">
              <Link to="/signup">Yeniden kaydol</Link>
            </Button>
          </div>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <div className="space-y-5">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">Hesabınızı etkinleştirin</h1>
          <p className="text-md text-muted-foreground leading-relaxed">
            Hesabınız ve organizasyonunuz bu düğmeye bastığınızda oluşturulur; ardından doğrudan
            organizasyonunuza girersiniz.
          </p>
        </div>
        <FormError message={verify.error ? errorMessage(verify.error) : undefined} />
        <Button
          className="h-10 w-full"
          disabled={verify.isPending || verify.isSuccess}
          onClick={() => verify.mutate({ token })}
        >
          {verify.isPending || verify.isSuccess ? "Etkinleştiriliyor…" : "Hesabımı etkinleştir"}
        </Button>
      </div>
    </AuthLayout>
  );
}
