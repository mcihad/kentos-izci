// The channel between sensitive calls and the app-wide re-authentication
// dialog. It has no dependencies, so the root loads it without the dialog.

let open: (() => void) | undefined;
let pending: Promise<boolean> | undefined;
let settle: ((ok: boolean) => void) | undefined;

/** Registers the dialog that asks for re-authentication; returns the cleanup. */
export function registerReauthDialog(show: () => void): () => void {
  open = show;
  return () => {
    if (open === show) open = undefined;
  };
}

/**
 * Asks the user to confirm their identity and resolves to whether they did.
 * Requests made while the dialog is open share it.
 */
export function requestReauth(): Promise<boolean> {
  if (pending) return pending;
  if (!open) return Promise.resolve(false);
  pending = new Promise<boolean>((resolve) => {
    settle = resolve;
  });
  open();
  return pending;
}

/** Called by the dialog when it closes. */
export function finishReauth(ok: boolean) {
  const done = settle;
  pending = undefined;
  settle = undefined;
  done?.(ok);
}
