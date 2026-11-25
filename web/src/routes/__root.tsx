import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Link, Outlet } from "@tanstack/react-router";
import { ReauthHost } from "@/components/app/reauth-host";
import { Logo } from "@/components/brand/logo";
import { Button } from "@/components/ui/button";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Root,
  notFoundComponent: NotFound,
});

function Root() {
  return (
    <TooltipProvider delayDuration={300}>
      <Outlet />
      <ReauthHost />
      <Toaster position="bottom-right" />
    </TooltipProvider>
  );
}

function NotFound() {
  return (
    <main className="grid min-h-dvh place-items-center px-6">
      <div className="flex max-w-sm flex-col items-center gap-5 text-center">
        <Logo className="size-10" />
        <div className="space-y-1.5">
          <h1 className="font-semibold text-xl tracking-tight">Bu sayfa yok</h1>
          <p className="text-muted-foreground text-sm">
            Bağlantı eskimiş ya da adres yanlış yazılmış olabilir.
          </p>
        </div>
        <Button asChild variant="outline">
          <Link to="/">Ana sayfaya dön</Link>
        </Button>
      </div>
    </main>
  );
}
