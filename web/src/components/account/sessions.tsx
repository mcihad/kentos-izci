import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { UserService } from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage } from "@/lib/errors";

export function Sessions() {
  const qc = useQueryClient();
  const list = useQuery(UserService.method.listSessions, {}, { transport: baseTransport });
  const revoke = useMutation(UserService.method.revokeSession, {
    transport: baseTransport,
    onSuccess: (_, req) => {
      toast.success(req.allOthers ? "Diğer tüm oturumlar kapatıldı" : "Oturum kapatıldı");
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: UserService.method.listSessions,
          transport: baseTransport,
          cardinality: "finite",
        }),
      });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  if (!list.data) return <Skeleton className="my-6 h-48 w-full max-w-3xl" />;
  const others = list.data.sessions.filter((s) => !s.current).length;
  return (
    <div className="max-w-3xl py-6">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="font-semibold text-lg">Açık oturumlar</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Tanımadığınız bir oturum görürseniz kapatın ve parolanızı değiştirin.
          </p>
        </div>
        {others > 0 ? (
          <Button
            variant="outline"
            size="sm"
            disabled={revoke.isPending}
            onClick={() => revoke.mutate({ allOthers: true })}
          >
            Diğer oturumları kapat
          </Button>
        ) : null}
      </div>
      <ul className="divide-y rounded-lg border">
        {list.data.sessions.map((s) => (
          <li key={s.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-md">
                {s.client || "Bilinmeyen istemci"}
                {s.current ? <span className="ml-2 text-primary text-xs">bu cihaz</span> : null}
              </p>
              <p className="text-xs text-muted-foreground">
                <span className="font-mono">{s.ipAddress}</span>, son etkinlik{" "}
                <RelativeTime value={s.lastSeenAt} />, açılış <RelativeTime value={s.createdAt} />
              </p>
            </div>
            {!s.current ? (
              <Button
                variant="ghost"
                size="sm"
                disabled={revoke.isPending}
                onClick={() => revoke.mutate({ id: s.id })}
              >
                Kapat
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}
