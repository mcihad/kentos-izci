import { Code, ConnectError, type Interceptor, type Transport } from "@connectrpc/connect";
import { addStaticKeyToTransport } from "@connectrpc/connect-query";
import { createConnectTransport } from "@connectrpc/connect-web";

const baseUrl = typeof window !== "undefined" ? window.location.origin : "http://localhost";

/**
 * Called when any API call reports that the session is gone, so the app can
 * send the user to the login page once instead of every screen showing its
 * own error.
 */
let onUnauthenticated: (() => void) | undefined;

export function setUnauthenticatedHandler(fn: () => void) {
  onUnauthenticated = fn;
}

const sessionWatcher: Interceptor = (next) => async (req) => {
  try {
    return await next(req);
  } catch (err) {
    if (
      err instanceof ConnectError &&
      err.code === Code.Unauthenticated &&
      !req.url.includes("AuthService/")
    ) {
      onUnauthenticated?.();
    }
    throw err;
  }
};

function orgHeader(org: string): Interceptor {
  return (next) => (req) => {
    req.header.set("Izci-Org", org);
    return next(req);
  };
}

function create(key: string, interceptors: Interceptor[]): Transport {
  return addStaticKeyToTransport(
    createConnectTransport({ baseUrl, useBinaryFormat: true, interceptors }),
    key,
  );
}

/** Transport for calls that are not scoped to an organization. */
export const baseTransport = create("base", [sessionWatcher]);

const orgTransports = new Map<string, Transport>();

/**
 * Transport that sends the Izci-Org header. One transport per organization
 * keeps query caches of different organizations apart (Connect-Query keys
 * include the transport).
 */
export function orgTransport(org: string): Transport {
  let t = orgTransports.get(org);
  if (!t) {
    t = create(`org:${org}`, [sessionWatcher, orgHeader(org)]);
    orgTransports.set(org, t);
  }
  return t;
}
