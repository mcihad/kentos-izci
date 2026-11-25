import type { DescMessage, DescMethodUnary, MessageInitShape, MessageShape } from "@bufbuild/protobuf";
import { timestampMs } from "@bufbuild/protobuf/wkt";
import type { ConnectError } from "@connectrpc/connect";
import { type UseMutationOptions, useTransport } from "@connectrpc/connect-query";
import { useMutation } from "@tanstack/react-query";
import type { Session } from "@/gen/izci/api/v1/auth_pb";
import { ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { errorReason } from "./errors";
import { requestReauth } from "./reauth-bus";

export { requestReauth };

// The server wants sensitive changes from a session that proved its user's
// identity in the last 10 minutes (auth.ReauthWindow); a minute less here
// leaves room for clock skew and slow requests.
const recentMs = 9 * 60_000;

/** Whether the session proved its user's identity recently enough for sensitive changes. */
export function recentlyAuthenticated(session: Session | undefined): boolean {
  const at = session?.reauthenticatedAt;
  return at !== undefined && Date.now() - timestampMs(at) < recentMs;
}

/** Runs a call; when the server wants a recent re-authentication, asks the user and retries once. */
export async function withReauth<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (errorReason(err) !== ErrorReason.REAUTHENTICATION_REQUIRED || !(await requestReauth())) throw err;
    return call();
  }
}

/**
 * useMutation for sensitive changes (access tokens, two-factor setup,
 * organization security): when the server wants it, the user confirms
 * their identity and the call is retried. Cancelling ends in the
 * REAUTHENTICATION_REQUIRED error.
 */
export function useSensitiveMutation<I extends DescMessage, O extends DescMessage>(
  schema: DescMethodUnary<I, O>,
  { transport, ...options }: UseMutationOptions<I, O> = {},
) {
  const fromContext = useTransport();
  const t = transport ?? fromContext;
  return useMutation<MessageShape<O>, ConnectError, MessageInitShape<I>>({
    ...options,
    mutationFn: (input) =>
      withReauth(async () => (await t.unary(schema, undefined, undefined, undefined, input)).message),
  });
}
