import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { createQueryOptions, useQuery } from "@connectrpc/connect-query";
import type { QueryClient } from "@tanstack/react-query";
import { AuthService, GetSessionResponseSchema, type Session } from "@/gen/izci/api/v1/auth_pb";
import { Role } from "@/gen/izci/api/v1/common_pb";
import { baseTransport } from "./api";

export const sessionQuery = createQueryOptions(
  AuthService.method.getSession,
  {},
  { transport: baseTransport },
);

/** Loads the current session; null when nobody is signed in. */
export async function loadSession(qc: QueryClient): Promise<Session | null> {
  try {
    const res = await qc.ensureQueryData(sessionQuery);
    return res.session ?? null;
  } catch (err) {
    if (err instanceof ConnectError && err.code === Code.Unauthenticated) return null;
    throw err;
  }
}

/** Stores a session returned by login, setup or invitation calls. */
export function storeSession(qc: QueryClient, session: Session | undefined) {
  qc.setQueryData(sessionQuery.queryKey, create(GetSessionResponseSchema, { session }));
}

/** The signed-in session (available below the authenticated routes). */
export function useSession(): Session | undefined {
  return useQuery(AuthService.method.getSession, {}, { transport: baseTransport, staleTime: 60_000 }).data
    ?.session;
}

export function firstOrganization(session: Session | null | undefined): string | undefined {
  return session?.memberships[0]?.organization?.slug;
}

/** Members and above may triage and comment. */
export function canWrite(role: Role | undefined): boolean {
  return (role ?? Role.UNSPECIFIED) >= Role.MEMBER;
}

/** Admins and owners manage projects, keys and members. */
export function canAdmin(role: Role | undefined): boolean {
  return (role ?? Role.UNSPECIFIED) >= Role.ADMIN;
}
