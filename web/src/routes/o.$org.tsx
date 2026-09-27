import { TransportProvider } from "@connectrpc/connect-query";
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { useMemo } from "react";
import { AppShell } from "@/components/app/app-shell";
import { TwoFactorGate } from "@/components/app/two-factor-gate";
import { orgTransport } from "@/lib/api";
import { OrgContext } from "@/lib/org";
import { firstOrganization, loadSession, useSession } from "@/lib/session";

export const Route = createFileRoute("/o/$org")({
  beforeLoad: async ({ context, params, location }) => {
    const session = await loadSession(context.queryClient);
    if (!session) throw redirect({ to: "/login", search: { redirect: location.href } });
    const membership = session.memberships.find((m) => m.organization?.slug === params.org);
    if (!membership) {
      const first = firstOrganization(session);
      throw redirect(first ? { to: "/o/$org", params: { org: first } } : { to: "/" });
    }
    return { membership };
  },
  component: OrgLayout,
});

function OrgLayout() {
  const { org } = Route.useParams();
  const initial = Route.useRouteContext().membership;
  const session = useSession();
  const membership = session?.memberships.find((m) => m.organization?.slug === org) ?? initial;
  const value = useMemo(
    () => ({
      slug: org,
      name: membership.organization?.name ?? org,
      role: membership.role,
      transport: orgTransport(org),
    }),
    [org, membership],
  );
  // The server turns every call away until two-factor authentication is on.
  if (membership.requiresTwoFactor && session?.user?.twoFactorEnabled === false) {
    return <TwoFactorGate orgName={value.name} />;
  }
  return (
    <OrgContext.Provider value={value}>
      <TransportProvider transport={value.transport}>
        <AppShell>
          <Outlet />
        </AppShell>
      </TransportProvider>
    </OrgContext.Provider>
  );
}
