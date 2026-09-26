import { useQuery } from "@connectrpc/connect-query";
import type * as React from "react";
import { Wordmark } from "@/components/brand/logo";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";

/** Quiet single-column frame for sign-in, setup and invitation pages. */
export function AuthLayout({ children, wide = false }: { children: React.ReactNode; wide?: boolean }) {
  const setup = useQuery(AuthService.method.getSetupState, {}, { staleTime: Number.POSITIVE_INFINITY });
  return (
    <div className="flex min-h-dvh flex-col px-5 py-8 sm:px-8">
      <header className="mx-auto w-full max-w-5xl">
        <Wordmark />
      </header>
      <main className="flex flex-1 items-center justify-center py-10">
        <div className={wide ? "w-full max-w-lg" : "w-full max-w-[22rem]"}>{children}</div>
      </main>
      <footer className="mx-auto w-full max-w-5xl text-muted-foreground text-xs">
        KentOS İzci {setup.data?.version ? <span className="tabular">{setup.data.version}</span> : null}
      </footer>
    </div>
  );
}
