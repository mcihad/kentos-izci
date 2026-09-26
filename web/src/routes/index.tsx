import { createQueryOptions } from "@connectrpc/connect-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { AuthLayout } from "@/components/app/auth-layout";
import { CreateOrganizationForm } from "@/components/app/create-organization";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { baseTransport } from "@/lib/api";
import { firstOrganization, loadSession } from "@/lib/session";

export const Route = createFileRoute("/")({
  beforeLoad: async ({ context }) => {
    const session = await loadSession(context.queryClient);
    if (!session) {
      const setup = await context.queryClient.ensureQueryData(
        createQueryOptions(AuthService.method.getSetupState, {}, { transport: baseTransport }),
      );
      throw redirect({ to: setup.setupRequired ? "/setup" : "/login" });
    }
    const org = firstOrganization(session);
    if (org) throw redirect({ to: "/o/$org", params: { org } });
  },
  component: NoOrganization,
});

/** Signed in, but not a member of any organization. */
function NoOrganization() {
  return (
    <AuthLayout>
      <div className="space-y-6">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">Bir organizasyona katılın</h1>
          <p className="text-muted-foreground text-sm leading-relaxed">
            Hesabınız henüz bir organizasyona bağlı değil. Ekibinizden davet isteyin ya da yeni bir
            organizasyon oluşturun.
          </p>
        </div>
        <CreateOrganizationForm />
      </div>
    </AuthLayout>
  );
}
