import { skipToken, useQuery } from "@connectrpc/connect-query";
import { CopyButton } from "@kentos/components/common/copy-button";
import {
  Drawer,
  DrawerBody,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from "@kentos/components/common/drawer";
import { useIsMobile } from "@kentos/components/lib/use-mobile";
import { Button } from "@kentos/components/ui/button";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { Link } from "@tanstack/react-router";
import { ArrowUpRightIcon } from "lucide-react";
import { createContext, type ReactNode, useContext, useMemo, useRef, useState } from "react";
import { ErrorState } from "@/components/common/states";
import { StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { IssueService } from "@/gen/izci/api/v1/issue_pb";
import { platformLabel } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import {
  IssueTitle,
  LevelBlaze,
  LevelLabel,
  PriorityBadge,
  StatusBadge,
  SubstatusBadge,
  UnhandledBadge,
} from "./badges";
import { IssueAside } from "./issue-aside";
import { TriageBar } from "./triage-bar";

/**
 * An issue's summary in a drawer beside the list: title, status, triage
 * actions, charts and facts, with a link to the full page. It asks for the
 * same data as the issue page, so opening the full page is instant.
 *
 * On desktop the drawer is not modal: the list stays usable, a click on
 * another row switches the summary and Escape closes it. On phones it is a
 * bottom sheet.
 */
export function IssuePreview({
  shortId,
  onClose,
  writable,
}: {
  /** The issue to show, or null when closed. */
  shortId: string | null;
  onClose: () => void;
  writable: boolean;
}) {
  const isMobile = useIsMobile();
  // Keep showing the last issue while the drawer slides out.
  const last = useRef(shortId);
  if (shortId) last.current = shortId;
  const shown = shortId ?? last.current;
  return (
    <Drawer open={shortId !== null} onOpenChange={(open) => !open && onClose()} modal={isMobile}>
      <DrawerContent
        side="right"
        size="lg"
        mobileSize="xl"
        resizable
        sizeStorageKey="izci-issue-preview"
        // The list stays the place of work: clicks on it do not close the
        // drawer and focus stays where it was.
        onInteractOutside={(e) => {
          if (!isMobile) e.preventDefault();
        }}
        onOpenAutoFocus={(e) => {
          if (!isMobile) e.preventDefault();
        }}
      >
        {shown ? <PreviewBody key={shown} shortId={shown} writable={writable} /> : null}
      </DrawerContent>
    </Drawer>
  );
}

const PreviewContext = createContext<((shortId: string) => void) | null>(null);

/** Opens an issue's summary drawer from inside an IssuePreviewHost, or null outside one. */
export function useIssuePreview() {
  return useContext(PreviewContext);
}

/**
 * Gives every IssueRow below it the summary drawer: a plain click on a row
 * opens the issue's summary here instead of leaving the page.
 */
export function IssuePreviewHost({ writable, children }: { writable: boolean; children: ReactNode }) {
  const [shortId, setShortId] = useState<string | null>(null);
  const open = useMemo(() => (id: string) => setShortId(id), []);
  return (
    <PreviewContext.Provider value={open}>
      {children}
      <IssuePreview shortId={shortId} onClose={() => setShortId(null)} writable={writable} />
    </PreviewContext.Provider>
  );
}

function PreviewBody({ shortId, writable }: { shortId: string; writable: boolean }) {
  const org = useOrg();
  const q = useQuery(IssueService.method.getIssue, {
    issue: shortId,
    statsPeriod: StatsPeriod.LAST_24_HOURS,
  });
  const issue = q.data?.issue;
  const s = issue?.summary;
  const facets = useQuery(IssueService.method.getIssueTagFacets, s ? { issueId: s.id } : skipToken, {
    staleTime: 60_000,
  });

  if (q.isPending) {
    return (
      <>
        <DrawerHeader>
          <DrawerTitle className="sr-only">{shortId} özeti</DrawerTitle>
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-6 w-4/5" />
          <Skeleton className="h-4 w-1/2" />
        </DrawerHeader>
        <DrawerBody className="grid content-start gap-4">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-28 w-full" />
          <Skeleton className="h-40 w-full" />
        </DrawerBody>
      </>
    );
  }
  if (!issue || !s) {
    return (
      <>
        <DrawerHeader>
          <DrawerTitle>{shortId}</DrawerTitle>
        </DrawerHeader>
        <DrawerBody>
          <ErrorState error={q.error} onRetry={() => void q.refetch()} />
        </DrawerBody>
      </>
    );
  }

  return (
    <>
      <DrawerHeader>
        <div className="flex gap-3">
          <LevelBlaze level={s.level} className="w-1 self-stretch" />
          <div className="grid min-w-0 flex-1 gap-1">
            <div className="flex items-center gap-1 text-muted-foreground text-sm">
              <span className="font-medium font-mono text-foreground">{s.shortId}</span>
              <CopyButton value={s.permalink} label="Bağlantıyı kopyala" className="size-7" />
            </div>
            <DrawerTitle className="break-words text-lg leading-snug">
              <IssueTitle type={s.type} value={s.value} title={s.title} />
            </DrawerTitle>
            {s.culprit ? (
              <DrawerDescription className="break-words font-mono">{s.culprit}</DrawerDescription>
            ) : (
              <DrawerDescription className="sr-only">Hata özeti</DrawerDescription>
            )}
            <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-muted-foreground text-sm">
              <StatusBadge status={s.status} />
              <SubstatusBadge substatus={s.substatus} />
              {s.unhandled ? <UnhandledBadge /> : null}
              <PriorityBadge priority={s.priority} />
              <LevelLabel level={s.level} />
              <span>{s.project?.name}</span>
              <span>{platformLabel(s.platform)}</span>
            </div>
          </div>
        </div>
      </DrawerHeader>
      <DrawerBody className="grid content-start gap-6">
        <TriageBar issue={issue} writable={writable} />
        <IssueAside issue={issue} facets={facets.data?.facets} writable={writable} />
      </DrawerBody>
      <DrawerFooter>
        <Button asChild>
          <Link to="/o/$org/issues/$issueId" params={{ org: org.slug, issueId: s.shortId }}>
            Tam sayfayı aç
            <ArrowUpRightIcon />
          </Link>
        </Button>
      </DrawerFooter>
    </>
  );
}
