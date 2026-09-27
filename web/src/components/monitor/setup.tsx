import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { Section } from "@/components/app/page";
import { CodeBlock } from "@/components/common/code-block";
import { Segmented } from "@/components/common/segmented";
import { Skeleton } from "@/components/ui/skeleton";
import { type Monitor, MonitorIntervalUnit } from "@/gen/izci/api/v1/monitor_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";

type Lang = "dotnet" | "javascript" | "python" | "go" | "http";

const units: Record<MonitorIntervalUnit, string> = {
  [MonitorIntervalUnit.UNSPECIFIED]: "hour",
  [MonitorIntervalUnit.MINUTE]: "minute",
  [MonitorIntervalUnit.HOUR]: "hour",
  [MonitorIntervalUnit.DAY]: "day",
  [MonitorIntervalUnit.WEEK]: "week",
  [MonitorIntervalUnit.MONTH]: "month",
  [MonitorIntervalUnit.YEAR]: "year",
};

const title = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

function snippets(m: Monitor, base: string, publicId: string): Record<Lang, { code: string; lang: string }> {
  const s = m.schedule?.kind;
  const cron = s?.case === "crontab" ? s.value : "";
  const every = s?.case === "interval" ? s.value.value : 1;
  const unit = s?.case === "interval" ? units[s.value.unit] : "hour";
  const tz = m.timezone;
  const margin = m.checkinMarginMinutes;
  const runtime = m.maxRuntimeMinutes;
  const url = `${base}/api/${publicId}/cron/${m.slug}/<anahtar>/`;
  return {
    dotnet: {
      lang: "csharp",
      code: `var checkInId = SentrySdk.CaptureCheckIn("${m.slug}", CheckInStatus.InProgress,
    configureMonitorOptions: o =>
    {
        ${cron ? `o.Interval("${cron}");` : `o.Interval(${every}, SentryMonitorInterval.${title(unit)});`}
        o.TimeZone = "${tz}";
        o.CheckInMargin = TimeSpan.FromMinutes(${margin});
        o.MaxRuntime = TimeSpan.FromMinutes(${runtime});
    });
try
{
    await RunJobAsync();
    SentrySdk.CaptureCheckIn("${m.slug}", CheckInStatus.Ok, checkInId);
}
catch
{
    SentrySdk.CaptureCheckIn("${m.slug}", CheckInStatus.Error, checkInId);
    throw;
}`,
    },
    javascript: {
      lang: "javascript",
      code: `import * as Sentry from "@sentry/node";

await Sentry.withMonitor("${m.slug}", () => runJob(), {
  schedule: ${cron ? `{ type: "crontab", value: "${cron}" }` : `{ type: "interval", value: ${every}, unit: "${unit}" }`},
  timezone: "${tz}",
  checkinMargin: ${margin},
  maxRuntime: ${runtime},
});`,
    },
    python: {
      lang: "python",
      code: `from sentry_sdk.crons import monitor

@monitor(monitor_slug="${m.slug}", monitor_config={
    "schedule": ${cron ? `{"type": "crontab", "value": "${cron}"}` : `{"type": "interval", "value": ${every}, "unit": "${unit}"}`},
    "timezone": "${tz}",
    "checkin_margin": ${margin},
    "max_runtime": ${runtime},
})
def run_job():
    ...`,
    },
    go: {
      lang: "go",
      code: `err := izci.WithMonitor(ctx, "${m.slug}", &izci.MonitorConfig{
	${cron ? `Crontab: "${cron}",` : `Interval: ${every}, Unit: izci.${title(unit)},`}
	Timezone:   "${tz}",
	MaxRuntime: ${runtime} * time.Minute,
}, runJob)`,
    },
    http: {
      lang: "shellscript",
      code: `# When the job starts
curl -X POST '${url}?status=in_progress'
# When it ends: status=ok on success, status=error on failure
curl -X POST '${url}?status=ok'`,
    },
  };
}

/** How to report runs of this monitor, for each supported client. */
export function MonitorSetup({ monitor }: { monitor: Monitor }) {
  const ep = useQuery(ProjectService.method.getIngestEndpoints, { project: monitor.project?.slug ?? "" });
  const [lang, setLang] = useState<Lang>("dotnet");
  const publicId = ep.data?.sentryDsnTemplate.split("/").pop() ?? "";
  return (
    <Section
      title="Çalışmaları bildirme"
      description="Görev başlarken ve biterken check-in gönderir. Sentry SDK'ları ayarları olduğu gibi kullanır; anahtar olarak projenin API anahtarını verin."
    >
      {ep.data ? (
        <div className="grid grid-cols-1 gap-3">
          <Segmented
            label="İstemci"
            value={lang}
            onChange={setLang}
            className="max-w-xl"
            options={[
              { value: "dotnet", label: ".NET" },
              { value: "javascript", label: "Node.js" },
              { value: "python", label: "Python" },
              { value: "go", label: "Go" },
              { value: "http", label: "HTTP" },
            ]}
          />
          <CodeBlock {...snippets(monitor, ep.data.baseUrl, publicId)[lang]} maxHeight="24rem" />
        </div>
      ) : (
        <Skeleton className="h-48 w-full" />
      )}
    </Section>
  );
}
