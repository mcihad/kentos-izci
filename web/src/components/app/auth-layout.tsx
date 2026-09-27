import { useQuery } from "@connectrpc/connect-query";
import type * as React from "react";
import { Logo, Wordmark } from "@/components/brand/logo";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";

const features = ["Sentry SDK'larıyla uyumlu", "Telegram bildirimleri", "Yapay zekâ ile kök neden"];
const protocols = ["gRPC", "Connect", "Sentry", "KVKK uyumlu"];

/**
 * Sign-in, setup and invitation frame, after the design system's login
 * screen: a navy panel that says what İzci does, and the form on the card
 * surface. Phones get the form alone.
 */
export function AuthLayout({ children, wide = false }: { children: React.ReactNode; wide?: boolean }) {
  const setup = useQuery(AuthService.method.getSetupState, {}, { staleTime: Number.POSITIVE_INFINITY });
  return (
    <div className="grid min-h-dvh bg-card lg:grid-cols-[minmax(0,1fr)_minmax(28rem,34rem)]">
      <aside className="on-dark relative hidden flex-col justify-between overflow-hidden bg-(--navy-900) p-12 text-white lg:flex">
        <div className="flex items-center gap-3">
          <span className="grid size-10 place-items-center rounded-md bg-white/15">
            <Logo inverse className="size-6" />
          </span>
          <span className="font-bold text-3xl leading-none tracking-[-0.035em]">İzci</span>
          <span className="font-medium text-(--sidebar-fg-muted) text-2xs uppercase tracking-(--tracking-caps)">
            KentOS
          </span>
        </div>
        <div className="grid max-w-lg gap-5">
          <h1 className="font-semibold text-[2.375rem] text-white leading-[1.15]">
            Uygulamalarınızın hataları tek yerde
          </h1>
          <p className="text-(--navy-100)/80 text-lg leading-relaxed">
            KentOS uygulamalarından gelen hatalar gruplanır, hangi müşteriyi etkilediği görülür ve ekibinize
            anında haber verilir.
          </p>
          <ul className="flex flex-wrap gap-2">
            {features.map((f) => (
              <li
                key={f}
                className="inline-flex h-6 items-center rounded-full bg-white/12 px-2.5 font-semibold text-2xs text-white"
              >
                {f}
              </li>
            ))}
          </ul>
        </div>
        <ul className="flex flex-wrap gap-x-6 gap-y-1 font-mono text-(--sidebar-fg-muted) text-2xs">
          {protocols.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      </aside>
      <div className="flex min-h-dvh flex-col px-5 py-8 sm:px-10">
        <header className="lg:hidden">
          <Wordmark />
        </header>
        <main className="flex flex-1 items-center justify-center py-10">
          <div className={wide ? "w-full max-w-lg" : "w-full max-w-[24rem]"}>{children}</div>
        </main>
        <footer className="text-muted-foreground text-sm lg:text-center">
          KentOS İzci {setup.data?.version ? <span className="tabular">{setup.data.version}</span> : null}
        </footer>
      </div>
    </div>
  );
}
