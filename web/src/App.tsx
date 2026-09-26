import { Activity, CircleCheck, CircleX } from "lucide-react";
import { useEffect, useState } from "react";

type Health = { status: string; version: string; commit: string };

/** Temporary landing screen until the application shell lands (phase F8). */
export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    fetch("/healthz")
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then(setHealth)
      .catch(() => setFailed(true));
  }, []);

  return (
    <main className="grid min-h-dvh place-items-center bg-background px-6 text-foreground">
      <div className="flex max-w-md flex-col items-center gap-6 text-center">
        <img src="/favicon.svg" alt="" className="size-16 rounded-2xl shadow-lg shadow-primary/10" />
        <div className="space-y-2">
          <h1 className="font-semibold text-3xl tracking-tight">İzci</h1>
          <p className="text-muted-foreground">KentOS uygulamaları için hata izleme ve toplama platformu</p>
        </div>
        <div className="flex items-center gap-2 rounded-full border bg-card px-4 py-1.5 font-mono text-muted-foreground text-xs">
          {health ? (
            <>
              <CircleCheck className="size-3.5 text-primary" />
              sunucu çalışıyor · {health.version}
              {health.commit ? ` · ${health.commit.slice(0, 7)}` : ""}
            </>
          ) : failed ? (
            <>
              <CircleX className="size-3.5 text-destructive" />
              sunucuya ulaşılamıyor
            </>
          ) : (
            <>
              <Activity className="size-3.5 animate-pulse" />
              bağlanıyor…
            </>
          )}
        </div>
      </div>
    </main>
  );
}
