import { Link } from "@tanstack/react-router";
import { InfoIcon } from "lucide-react";
import { useState } from "react";
import { FeedbackItem } from "@/components/feedback/feedback-list";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { EventDetail } from "@/gen/izci/api/v1/event_pb";
import type { Exception, Thread } from "@/gen/izci/ingest/v1/event_pb";
import { toDate } from "@/lib/format";
import { levels } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { Attachments } from "./attachments";
import { BreadcrumbTrail } from "./breadcrumbs";
import { ContextGrid } from "./contexts";
import { EventSection, JsonTree, KVList } from "./parts";
import { RequestView } from "./request";
import { StackTrail } from "./stack-trail";

/** The complete, readable form of one event. */
export function EventView({ detail }: { detail: EventDetail }) {
  const org = useOrg();
  const ev = detail.event;
  const hasApp = !!(
    ev?.exceptions.some((e) => e.stacktrace?.frames.some((f) => f.inApp === true)) ||
    ev?.threads.some((t) => t.stacktrace?.frames.some((f) => f.inApp === true))
  );
  const [appOnly, setAppOnly] = useState(true);
  const [newestFirst, setNewestFirst] = useState(true);
  if (!ev) return null;
  const color = levels[ev.level].color;
  const chain = [...ev.exceptions].reverse();
  // Without exception frames, the crashed thread (or the capturing thread of
  // a message or of an error without a trace) is the stack to show.
  const framed = (t: Thread) => (t.stacktrace?.frames.length ?? 0) > 0;
  const primaryThread = ev.exceptions.some((e) => e.stacktrace?.frames.length)
    ? undefined
    : (ev.threads.find((t) => t.crashed && framed(t)) ?? ev.threads.find((t) => t.current && framed(t)));
  const otherThreads = ev.threads.filter((t) => t !== primaryThread && framed(t));
  const extra = ev.extra ?? null;
  const tags = Object.entries(ev.tags).sort(([a], [b]) => a.localeCompare(b));
  const title = chain[0] ? `${chain[0].type}: ${chain[0].value}` : ev.message?.formatted || "";

  const trailToggles = (
    <>
      {hasApp ? (
        <div className="flex items-center gap-2">
          <Switch id="app-only" checked={appOnly} onCheckedChange={setAppOnly} />
          <Label htmlFor="app-only" className="font-normal text-[12.5px] text-muted-foreground">
            Yalnız uygulama kodu
          </Label>
        </div>
      ) : null}
      <div className="flex items-center gap-2">
        <Switch id="newest-first" checked={newestFirst} onCheckedChange={setNewestFirst} />
        <Label htmlFor="newest-first" className="font-normal text-[12.5px] text-muted-foreground">
          En yeni çağrı üstte
        </Label>
      </div>
    </>
  );

  return (
    <div className="@container">
      {ev.message?.formatted && chain.length === 0 ? (
        <EventSection title="Mesaj">
          <p className="whitespace-pre-wrap break-words font-mono text-[13px]">{ev.message.formatted}</p>
          {ev.message.template && ev.message.template !== ev.message.formatted ? (
            <p className="mt-2 font-mono text-[12px] text-muted-foreground">
              Şablon: {ev.message.template}
              {ev.message.params.length ? `  (${ev.message.params.join(", ")})` : ""}
            </p>
          ) : null}
        </EventSection>
      ) : null}

      {chain.length > 0 ? (
        <EventSection
          title={chain.length > 1 ? `İstisna zinciri (${chain.length})` : "İstisna"}
          aside={trailToggles}
        >
          <div className="grid grid-cols-1 gap-6">
            {chain.map((ex, i) => (
              <ExceptionBlock
                // biome-ignore lint/suspicious/noArrayIndexKey: the chain order is fixed
                key={i}
                ex={ex}
                cause={i > 0}
                detail={detail}
                platform={ev.platform}
                color={i === 0 ? color : "var(--muted-foreground)"}
                appOnly={appOnly}
                newestFirst={newestFirst}
              />
            ))}
          </div>
        </EventSection>
      ) : null}

      {primaryThread ? (
        <EventSection
          title={
            primaryThread.crashed
              ? `Çöken iş parçacığı${primaryThread.name ? `: ${primaryThread.name}` : ""}`
              : "Yığın izi"
          }
          aside={trailToggles}
        >
          <StackTrail
            frames={primaryThread.stacktrace?.frames ?? []}
            detail={detail}
            platform={ev.platform}
            levelColor={color}
            appOnly={appOnly}
            newestFirst={newestFirst}
          />
        </EventSection>
      ) : null}

      {detail.feedback.length > 0 ? (
        <EventSection title="Kullanıcı geri bildirimi">
          <ul className="-my-4 divide-y">
            {detail.feedback.map((fb) => (
              <FeedbackItem key={fb.id} fb={fb} showIssue={false} />
            ))}
          </ul>
        </EventSection>
      ) : null}

      {detail.attachments.length > 0 ? (
        <EventSection title={`Ekler (${detail.attachments.length})`}>
          <Attachments files={detail.attachments} />
        </EventSection>
      ) : null}

      {ev.breadcrumbs.length > 0 ? (
        <EventSection title="Hatadan önceki adımlar">
          <BreadcrumbTrail
            crumbs={ev.breadcrumbs}
            eventTime={toDate(ev.timestamp)}
            errorTitle={title}
            levelColor={color}
          />
        </EventSection>
      ) : null}

      {ev.request && (ev.request.url || ev.request.method) ? (
        <EventSection title="İstek">
          <RequestView request={ev.request} />
        </EventSection>
      ) : null}

      {ev.user || ev.customer || ev.tenant ? (
        <EventSection title="Etkilenen">
          <KVList
            items={[
              [
                "Müşteri",
                ev.customer?.id ? (
                  <Link
                    key="customer"
                    to="/o/$org/customers/$key"
                    params={{ org: org.slug, key: ev.customer.id.toLowerCase() }}
                    className="underline-offset-4 hover:underline"
                  >
                    {ev.customer.name || ev.customer.id}
                  </Link>
                ) : undefined,
              ],
              ["Kiracı", ev.tenant ? [ev.tenant.name, ev.tenant.id].filter(Boolean).join(" — ") : undefined],
              ["Kullanıcı", ev.user?.name],
              ["E-posta", ev.user?.email],
              ["Kullanıcı adı", ev.user?.username],
              ["Kimlik", ev.user?.id, { mono: true }],
              ["IP adresi", detail.clientIp || ev.user?.ipAddress, { mono: true }],
              ["Segment", ev.user?.segment],
              [
                "Konum",
                ev.user?.geo
                  ? [ev.user.geo.city, ev.user.geo.region, ev.user.geo.countryCode].filter(Boolean).join(", ")
                  : undefined,
              ],
              ...Object.entries(ev.user?.attributes ?? {}).map(([k, v]) => [k, v] as [string, string]),
            ]}
          />
        </EventSection>
      ) : null}

      {ev.contexts || detail.userAgent ? (
        <EventSection title="Ortam">
          <ContextGrid contexts={ev.contexts} userAgent={detail.userAgent} />
        </EventSection>
      ) : null}

      {tags.length > 0 ? (
        <EventSection title="Etiketler">
          <div className="flex flex-wrap gap-1.5">
            {tags.map(([k, v]) => (
              <Link
                key={k}
                to="/o/$org/issues"
                params={{ org: org.slug }}
                search={{ query: `${k}:${/[\s"]/.test(v) ? JSON.stringify(v) : v}` }}
                className="inline-flex max-w-full items-center overflow-hidden rounded-md border text-[12px] transition-colors hover:border-foreground/30"
              >
                <span className="bg-muted/60 px-2 py-0.5 text-muted-foreground">{k}</span>
                <span className="truncate px-2 py-0.5 font-mono">{v}</span>
              </Link>
            ))}
          </div>
        </EventSection>
      ) : null}

      {extra && typeof extra === "object" && Object.keys(extra).length > 0 ? (
        <EventSection title="Ek veriler">
          <div className="font-mono text-[12.5px] leading-relaxed">
            <JsonTree value={extra} />
          </div>
        </EventSection>
      ) : null}

      {otherThreads.length > 0 ? (
        <ThreadList threads={otherThreads} detail={detail} platform={ev.platform} />
      ) : null}

      <EventSection title="Teknik ayrıntılar">
        <KVList
          items={[
            ["Olay kimliği", ev.eventId, { mono: true }],
            ["Platform", ev.platform],
            ["Ortam", ev.environment],
            [
              "Sürüm",
              ev.release ? `${ev.release}${ev.dist ? ` (${ev.dist})` : ""}` : undefined,
              { mono: true },
            ],
            ["Sunucu", ev.serverName, { mono: true }],
            ["Logger", ev.logger, { mono: true }],
            ["İşlem", ev.transaction, { mono: true }],
            ["SDK", ev.sdk ? `${ev.sdk.name} ${ev.sdk.version}`.trim() : undefined, { mono: true }],
            ["Parmak izi", ev.fingerprint.length ? ev.fingerprint.join(", ") : undefined, { mono: true }],
          ]}
        />
        {detail.warnings.length > 0 ? (
          <div className="mt-4 rounded-md border px-3 py-2.5 text-[12.5px]">
            <p className="mb-1 flex items-center gap-1.5 font-medium">
              <InfoIcon className="size-3.5 text-muted-foreground" />
              Kayıt sırasında yapılan düzeltmeler
            </p>
            <ul className="list-disc space-y-0.5 pl-5 text-muted-foreground">
              {detail.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </div>
        ) : null}
        {Object.keys(ev.modules).length > 0 ? (
          <details className="mt-4 text-[12.5px]">
            <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
              Yüklü paketler ({Object.keys(ev.modules).length})
            </summary>
            <dl className="mt-2 grid grid-cols-[1fr_auto] gap-x-6 gap-y-0.5 font-mono text-[12px]">
              {Object.entries(ev.modules)
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([name, version]) => (
                  <div key={name} className="contents">
                    <dt className="truncate">{name}</dt>
                    <dd className="text-muted-foreground">{version}</dd>
                  </div>
                ))}
            </dl>
          </details>
        ) : null}
      </EventSection>
    </div>
  );
}

function ExceptionBlock({
  ex,
  cause,
  detail,
  platform,
  color,
  appOnly,
  newestFirst,
}: {
  ex: Exception;
  cause: boolean;
  detail: EventDetail;
  platform: string;
  color: string;
  appOnly: boolean;
  newestFirst: boolean;
}) {
  const m = ex.mechanism;
  return (
    <div>
      {cause ? <p className="mb-2 font-medium text-[12px] text-muted-foreground">Nedeni</p> : null}
      <div className="mb-3">
        <p className="break-words text-[14px]">
          <span className="font-semibold font-mono text-[13.5px]">{ex.type || "Hata"}</span>
          {ex.module ? (
            <span className="ml-2 font-mono text-[12px] text-muted-foreground">{ex.module}</span>
          ) : null}
        </p>
        {ex.value ? (
          <p className="mt-1 whitespace-pre-wrap break-words text-[13.5px] text-muted-foreground">
            {ex.value}
          </p>
        ) : null}
        {m ? (
          <p className="mt-1.5 flex flex-wrap gap-x-3 text-[12px] text-muted-foreground">
            {m.handled === false ? <span style={{ color: "var(--level-fatal)" }}>İşlenmemiş</span> : null}
            {m.handled === true ? <span>İşlendi</span> : null}
            {m.type ? <span className="font-mono">{m.type}</span> : null}
            {m.description ? <span>{m.description}</span> : null}
            {m.helpLink ? (
              <a href={m.helpLink} target="_blank" rel="noreferrer" className="underline underline-offset-4">
                Yardım
              </a>
            ) : null}
          </p>
        ) : null}
      </div>
      <StackTrail
        frames={ex.stacktrace?.frames ?? []}
        detail={detail}
        platform={platform}
        levelColor={color}
        appOnly={appOnly}
        newestFirst={newestFirst}
      />
    </div>
  );
}

function ThreadList({
  threads,
  detail,
  platform,
}: {
  threads: Thread[];
  detail: EventDetail;
  platform: string;
}) {
  return (
    <EventSection title={`Diğer iş parçacıkları (${threads.length})`}>
      <div className="grid grid-cols-1 gap-3">
        {threads.map((t) => (
          <details key={t.id || t.name} className="rounded-md border px-3 py-2">
            <summary className="cursor-pointer font-mono text-[12.5px]">
              {t.name || t.id}
              {t.state ? <span className="ml-2 text-muted-foreground">{t.state}</span> : null}
            </summary>
            <div className="mt-2">
              <StackTrail
                frames={t.stacktrace?.frames ?? []}
                detail={detail}
                platform={platform}
                levelColor="var(--muted-foreground)"
                appOnly={false}
                newestFirst
              />
            </div>
          </details>
        ))}
      </div>
    </EventSection>
  );
}
