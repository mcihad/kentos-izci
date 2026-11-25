import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { AtSignIcon, BellIcon, MessageSquareIcon, RotateCcwIcon, UserRoundCheckIcon } from "lucide-react";
import { useState } from "react";
import { RelativeTime } from "@/components/common/relative-time";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { type UserNotification, UserNotificationKind, UserService } from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { useOrg } from "@/lib/org";
import { cn } from "@/lib/utils";

const listKey = createConnectQueryKey({
  schema: UserService.method.listNotifications,
  transport: baseTransport,
  cardinality: undefined,
});

/** What happened, in a short sentence. */
function sentence(n: UserNotification): string {
  const actor = n.actor?.name || n.actor?.email || "";
  switch (n.kind) {
    case UserNotificationKind.ASSIGNED:
      if (actor) return `${actor} size atadı`;
      return n.text ? `Sahiplik kuralıyla size atandı (${n.text})` : "Size atandı";
    case UserNotificationKind.MENTIONED:
      return `${actor} sizi bir yorumda andı`;
    case UserNotificationKind.REGRESSED:
      return "Takip ettiğiniz hata geri döndü";
    case UserNotificationKind.COMMENTED:
      return `${actor} yorum yazdı`;
    default:
      return "Bildirim";
  }
}

const icons = {
  [UserNotificationKind.ASSIGNED]: UserRoundCheckIcon,
  [UserNotificationKind.MENTIONED]: AtSignIcon,
  [UserNotificationKind.REGRESSED]: RotateCcwIcon,
  [UserNotificationKind.COMMENTED]: MessageSquareIcon,
} as Record<UserNotificationKind, typeof BellIcon>;

/** The bell of the top bar: the member's personal notifications. */
export function NotificationsMenu() {
  const org = useOrg();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const list = useQuery(
    UserService.method.listNotifications,
    { pageSize: 20 },
    { transport: baseTransport, refetchInterval: 60_000, refetchOnWindowFocus: true },
  );
  const markRead = useMutation(UserService.method.markNotificationsRead, {
    transport: baseTransport,
    onSuccess: () => void qc.invalidateQueries({ queryKey: listKey }),
  });
  const unread = list.data?.unreadCount ?? 0;
  const items = list.data?.notifications ?? [];

  const openOne = (n: UserNotification) => {
    if (!n.read) markRead.mutate({ ids: [n.id] });
    setOpen(false);
    void navigate({
      to: "/o/$org/issues/$issueId",
      params: { org: n.organization?.slug ?? org.slug, issueId: n.issue?.shortId ?? "" },
    });
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="relative"
          aria-label={unread ? `Bildirimler, ${unread} okunmamış` : "Bildirimler"}
        >
          <BellIcon className="size-5 md:size-4" />
          {unread > 0 ? (
            <span className="absolute top-1 right-1 min-w-4 rounded-full bg-primary px-1 text-center font-semibold text-[10px] text-primary-foreground tabular leading-4">
              {unread > 99 ? "99+" : unread}
            </span>
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(24rem,calc(100vw-1rem))] p-0">
        <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
          <p className="font-semibold text-sm">Bildirimler</p>
          {unread > 0 ? (
            <Button
              variant="ghost"
              size="xs"
              disabled={markRead.isPending}
              onClick={() => markRead.mutate({ all: true })}
            >
              Tümünü okundu işaretle
            </Button>
          ) : null}
        </div>
        {items.length === 0 ? (
          <p className="px-4 py-6 text-center text-muted-foreground text-sm leading-relaxed">
            Size atanan hatalar, anıldığınız yorumlar ve takip ettiğiniz hatalardaki gelişmeler burada
            görünür.
          </p>
        ) : (
          <ul className="max-h-[min(28rem,70vh)] divide-y overflow-y-auto">
            {items.map((n) => {
              const Icon = icons[n.kind] ?? BellIcon;
              return (
                <li key={n.id}>
                  <button
                    type="button"
                    onClick={() => openOne(n)}
                    className={cn(
                      "flex w-full gap-2.5 px-3 py-2.5 text-left transition-colors duration-(--duration-fast) hover:bg-(--surface-hover) focus-visible:bg-(--surface-hover) focus-visible:outline-none",
                      !n.read && "bg-(--surface-muted)",
                    )}
                  >
                    <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <span className="grid min-w-0 flex-1 gap-0.5">
                      <span className="text-sm">{sentence(n)}</span>
                      <span className="truncate text-sm">
                        <span className="font-mono text-muted-foreground text-xs">{n.issue?.shortId}</span>{" "}
                        <span className="font-medium">{n.issue?.title}</span>
                      </span>
                      {n.text && n.kind !== UserNotificationKind.ASSIGNED ? (
                        <span className="line-clamp-2 text-muted-foreground text-xs">{n.text}</span>
                      ) : null}
                      <span className="text-muted-foreground text-xs">
                        {n.organization?.slug !== org.slug ? `${n.organization?.name} · ` : ""}
                        <RelativeTime value={n.createdAt} />
                      </span>
                    </span>
                    {!n.read ? (
                      <span className="mt-1.5 size-2 shrink-0 rounded-full bg-primary">
                        <span className="sr-only">Okunmadı</span>
                      </span>
                    ) : null}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
        <div className="border-t px-3 py-2">
          <Link
            to="/o/$org/account"
            params={{ org: org.slug }}
            search={{ tab: "notifications" }}
            onClick={() => setOpen(false)}
            className="text-link text-sm underline-offset-4 hover:underline"
          >
            Bildirim tercihleri
          </Link>
        </div>
      </PopoverContent>
    </Popover>
  );
}
