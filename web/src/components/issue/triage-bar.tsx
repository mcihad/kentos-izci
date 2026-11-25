import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { useQuery } from "@connectrpc/connect-query";
import { CheckIcon, ChevronDownIcon, EyeOffIcon, RotateCcwIcon, UserRoundIcon } from "lucide-react";
import { UserAvatar } from "@/components/common/user-avatar";
import { FollowButton } from "@/components/issue/follow-button";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { IssueStatus, Priority } from "@/gen/izci/api/v1/common_pb";
import type { Issue } from "@/gen/izci/api/v1/issue_pb";
import { OrganizationService } from "@/gen/izci/api/v1/organization_pb";
import { priorityLabels } from "@/lib/labels";
import { useTriage } from "@/lib/triage";

const hours = (h: number) => create(DurationSchema, { seconds: BigInt(h * 3600) });

/** Resolve, ignore, assign and prioritize one issue. */
export function TriageBar({ issue, writable }: { issue: Issue; writable: boolean }) {
  const s = issue.summary;
  const triage = useTriage();
  const members = useQuery(
    OrganizationService.method.listMembers,
    {},
    { enabled: writable, staleTime: 5 * 60_000 },
  );
  if (!s) return null;
  const ids = [s.id];
  const disabled = !writable || triage.pending;

  return (
    <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
      {s.status === IssueStatus.UNRESOLVED ? (
        <div className="flex">
          <Button
            size="sm"
            className="flex-1 rounded-r-none"
            disabled={disabled}
            onClick={() => void triage.resolve(ids)}
          >
            <CheckIcon className="size-4" />
            Çöz
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                size="sm"
                className="rounded-l-none border-primary-foreground/20 border-l px-1.5"
                disabled={disabled}
                aria-label="Çözme seçenekleri"
              >
                <ChevronDownIcon className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-64">
              <DropdownMenuItem onSelect={() => void triage.resolve(ids, { inNextRelease: true })}>
                Sonraki sürümde çözüldü
              </DropdownMenuItem>
              {issue.lastRelease ? (
                <DropdownMenuItem onSelect={() => void triage.resolve(ids, { inRelease: issue.lastRelease })}>
                  <span className="truncate">
                    <span className="font-mono">{issue.lastRelease}</span> sürümünde çözüldü
                  </span>
                </DropdownMenuItem>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ) : (
        <Button
          size="sm"
          variant="outline"
          className="max-sm:col-span-2"
          disabled={disabled}
          onClick={() => void triage.reopen(ids)}
        >
          <RotateCcwIcon className="size-4" />
          {s.status === IssueStatus.RESOLVED ? "Yeniden aç" : "Yoksaymayı kaldır"}
        </Button>
      )}
      {s.status === IssueStatus.UNRESOLVED ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="outline" disabled={disabled}>
              <EyeOffIcon className="size-4" />
              Yoksay
              <ChevronDownIcon className="size-4 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-64">
            <DropdownMenuItem onSelect={() => void triage.ignore(ids)}>Süresiz yoksay</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => void triage.ignore(ids, { untilEscalating: true })}>
              Artış görülene kadar
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="font-normal text-muted-foreground text-sm">
              Belirli bir süre
            </DropdownMenuLabel>
            <DropdownMenuItem onSelect={() => void triage.ignore(ids, { duration: hours(1) })}>
              1 saat
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => void triage.ignore(ids, { duration: hours(24) })}>
              24 saat
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => void triage.ignore(ids, { duration: hours(24 * 7) })}>
              1 hafta
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="font-normal text-muted-foreground text-sm">
              Olay sayısına göre
            </DropdownMenuLabel>
            <DropdownMenuItem onSelect={() => void triage.ignore(ids, { count: 100n })}>
              100 olay daha gelene kadar
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => void triage.ignore(ids, { count: 1000n })}>
              1.000 olay daha gelene kadar
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" variant="outline" disabled={disabled} className="min-w-0 sm:max-w-52">
            {s.assignee ? (
              <UserAvatar
                name={s.assignee.name || s.assignee.email}
                src={s.assignee.avatarUrl}
                className="size-4"
              />
            ) : (
              <UserRoundIcon className="size-4" />
            )}
            <span className="truncate">{s.assignee ? s.assignee.name || s.assignee.email : "Ata"}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          {s.assignee?.id !== triage.me ? (
            <DropdownMenuItem onSelect={() => void triage.assign(ids, triage.me)}>Bana ata</DropdownMenuItem>
          ) : null}
          {s.assignee ? (
            <DropdownMenuItem onSelect={() => void triage.assign(ids, "")}>Atamayı kaldır</DropdownMenuItem>
          ) : null}
          <DropdownMenuSeparator />
          <DropdownMenuLabel className="font-normal text-muted-foreground text-sm">Üyeler</DropdownMenuLabel>
          {members.data?.members.map((m) => (
            <DropdownMenuItem key={m.user?.id} onSelect={() => void triage.assign(ids, m.user?.id ?? "")}>
              <UserAvatar
                name={m.user?.name || m.user?.email || ""}
                src={m.user?.avatarUrl}
                className="size-5"
              />
              <span className="min-w-0 flex-1 truncate">{m.user?.name || m.user?.email}</span>
              {s.assignee?.id === m.user?.id ? <CheckIcon className="size-4 text-brand-ink" /> : null}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" variant="outline" disabled={disabled} className="min-w-0">
            <span className="truncate">Öncelik: {priorityLabels[s.priority] || "Orta"}</span>
            <ChevronDownIcon className="size-4 text-muted-foreground" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuRadioGroup
            value={String(s.priority)}
            onValueChange={(v) => void triage.setPriority(ids, Number(v) as Priority)}
          >
            {[Priority.CRITICAL, Priority.HIGH, Priority.MEDIUM, Priority.LOW].map((p) => (
              <DropdownMenuRadioItem key={p} value={String(p)}>
                {priorityLabels[p]}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      {/* Viewers may follow too. */}
      <FollowButton issue={issue} />
    </div>
  );
}
