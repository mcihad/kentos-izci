import "./fonts.css";
import "./index.css";

import { Code, ConnectError } from "@connectrpc/connect";
import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRouter, RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { baseTransport, setUnauthenticatedHandler } from "./lib/api";
import { routeTree } from "./routeTree.gen";

// Errors that will not go away by asking again.
const final = new Set([
  Code.InvalidArgument,
  Code.NotFound,
  Code.AlreadyExists,
  Code.PermissionDenied,
  Code.Unauthenticated,
  Code.FailedPrecondition,
  Code.Unimplemented,
]);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: (count, err) => !(err instanceof ConnectError && final.has(err.code)) && count < 2,
    },
  },
});

const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

// One redirect to the login page when the session expires, whichever call
// noticed it first.
let redirecting = false;
setUnauthenticatedHandler(() => {
  if (redirecting || router.state.location.pathname === "/login") return;
  redirecting = true;
  const here = router.state.location.href;
  queryClient.clear();
  void router.navigate({ to: "/login", search: { redirect: here } }).finally(() => {
    redirecting = false;
  });
});

const root = document.getElementById("root");
if (!root) throw new Error("#root element is missing");

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TransportProvider transport={baseTransport}>
        <RouterProvider router={router} />
      </TransportProvider>
    </QueryClientProvider>
  </StrictMode>,
);
