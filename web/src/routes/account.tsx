import { createFileRoute, redirect } from "@tanstack/react-router";
import { firstOrganization, loadSession } from "@/lib/session";

/**
 * Account pages without an organization in the address (links in e-mails):
 * opens them in the member's first organization.
 */
export const Route = createFileRoute("/account")({
  validateSearch: (s: Record<string, unknown>): { tab?: string } => ({
    tab: typeof s.tab === "string" ? s.tab : undefined,
  }),
  beforeLoad: async ({ context, search, location }) => {
    const session = await loadSession(context.queryClient);
    if (!session) throw redirect({ to: "/login", search: { redirect: location.href } });
    const org = firstOrganization(session);
    if (!org) throw redirect({ to: "/" });
    throw redirect({
      href: `/o/${org}/account${search.tab ? `?tab=${encodeURIComponent(search.tab)}` : ""}`,
    });
  },
});
