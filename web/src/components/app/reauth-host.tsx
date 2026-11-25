import { lazy, Suspense, useEffect, useState } from "react";
import { finishReauth, registerReauthDialog } from "@/lib/reauth-bus";

const ReauthDialog = lazy(() => import("./reauth-dialog"));
const OidcResult = lazy(() => import("./oidc-result"));

// The parameters oidc-result.tsx handles (oidcResultParams). The callback
// returns with a full page load, so the first address is enough.
const oidcResult = /[?&]oidc_(linked|reauth|error)=/;

/**
 * The app-wide re-authentication dialog and the outcome of identity
 * provider round trips made while signed in. Both load only when needed.
 */
export function ReauthHost() {
  const [open, setOpen] = useState(false);
  const [used, setUsed] = useState(false);
  const [returned, setReturned] = useState(() => oidcResult.test(window.location.search));
  useEffect(
    () =>
      registerReauthDialog(() => {
        setUsed(true);
        setOpen(true);
      }),
    [],
  );
  return (
    <Suspense fallback={null}>
      {used ? (
        <ReauthDialog
          open={open}
          onClose={(ok) => {
            setOpen(false);
            finishReauth(ok);
          }}
        />
      ) : null}
      {returned ? <OidcResult onDone={() => setReturned(false)} /> : null}
    </Suspense>
  );
}
