import type { Duration } from "@bufbuild/protobuf/wkt";
import type { ReactNode } from "react";
import { CodeBlock } from "@/components/common/code-block";
import type { UserAgentInfo } from "@/gen/izci/api/v1/event_pb";
import type { Contexts } from "@/gen/izci/ingest/v1/event_pb";
import { formatBytes, formatDateTime, formatDuration } from "@/lib/format";
import { JsonTree, KVList, Meter, Panel } from "./parts";

function dur(d: Duration | undefined): string | undefined {
  if (!d) return undefined;
  const ms = Number(d.seconds) * 1000 + d.nanos / 1e6;
  return ms > 0 ? formatDuration(ms) : undefined;
}

function bytes(n: bigint): string | undefined {
  return n > 0n ? formatBytes(n) : undefined;
}

function count(n: number): string | undefined {
  return n > 0 ? n.toLocaleString("tr-TR") : undefined;
}

function yesNo(v: boolean | undefined): string | undefined {
  return v === undefined ? undefined : v ? "Evet" : "Hayır";
}

/** Everything the application reported about where the error happened. */
export function ContextGrid({
  contexts: c,
  userAgent,
}: {
  contexts: Contexts | undefined;
  userAgent?: UserAgentInfo;
}) {
  if (!c && !userAgent) return null;
  const panels: ReactNode[] = [];
  const dev = c?.device;
  if (dev) {
    const cpu = dev.cpu;
    const mem = dev.memory;
    const memUsed =
      mem?.usedPercent ??
      (mem && mem.totalBytes > 0n && mem.availableBytes > 0n
        ? 100 - (Number(mem.availableBytes) / Number(mem.totalBytes)) * 100
        : undefined);
    panels.push(
      <Panel key="device" title="Cihaz">
        <div className="grid grid-cols-1 gap-3">
          {cpu?.usagePercent !== undefined ? (
            <Meter label="İşlemci kullanımı" value={cpu.usagePercent} />
          ) : null}
          {memUsed !== undefined ? <Meter label="Bellek kullanımı" value={memUsed} /> : null}
          <KVList
            items={[
              ["Model", [dev.manufacturer, dev.model || dev.name].filter(Boolean).join(" ")],
              ["Tür", dev.deviceType],
              ["Mimari", dev.arch, { mono: true }],
              [
                "İşlemci",
                cpu
                  ? [cpu.model, cpu.count ? `${cpu.count} çekirdek` : ""].filter(Boolean).join(", ")
                  : undefined,
              ],
              ["Frekans", cpu?.frequencyMhz ? `${cpu.frequencyMhz.toLocaleString("tr-TR")} MHz` : undefined],
              [
                "Yük ortalaması",
                cpu?.loadAverage.length
                  ? cpu.loadAverage
                      .map((v) => v.toLocaleString("tr-TR", { maximumFractionDigits: 2 }))
                      .join("  ")
                  : undefined,
                { mono: true },
              ],
              ["Toplam bellek", mem ? bytes(mem.totalBytes) : undefined],
              ["Boş bellek", mem ? bytes(mem.availableBytes || mem.freeBytes) : undefined],
              [
                "Takas",
                mem && mem.swapTotalBytes > 0n
                  ? `${bytes(mem.swapTotalBytes - mem.swapFreeBytes)} / ${bytes(mem.swapTotalBytes)}`
                  : undefined,
              ],
              ["Düşük bellek", mem?.lowMemory ? "Evet" : undefined],
              [
                "Disk",
                dev.storage && dev.storage.totalBytes > 0n
                  ? `${bytes(dev.storage.freeBytes)} boş / ${bytes(dev.storage.totalBytes)}`
                  : undefined,
              ],
              [
                "Pil",
                dev.batteryLevel !== undefined
                  ? `%${Math.round(dev.batteryLevel)}${dev.charging ? ", şarjda" : ""}`
                  : undefined,
              ],
              [
                "Ekran",
                dev.screen?.widthPixels ? `${dev.screen.widthPixels}×${dev.screen.heightPixels}` : undefined,
              ],
              ["Çevrim içi", yesNo(dev.online)],
              ["Açılış", dev.bootTime ? formatDateTime(dev.bootTime) : undefined],
            ]}
          />
        </div>
      </Panel>,
    );
  }
  if (c?.os?.name) {
    panels.push(
      <Panel key="os" title="İşletim sistemi">
        <KVList
          items={[
            ["Ad", `${c.os.name} ${c.os.version}`.trim()],
            ["Dağıtım", c.os.distribution],
            ["Yapı", c.os.build, { mono: true }],
            ["Çekirdek", c.os.kernelVersion, { mono: true }],
            ["Root", yesNo(c.os.rooted)],
          ]}
        />
      </Panel>,
    );
  }
  if (c?.runtime?.name) {
    panels.push(
      <Panel key="runtime" title="Çalışma ortamı">
        <KVList
          items={[
            ["Ad", `${c.runtime.name} ${c.runtime.version}`.trim()],
            ["Yapı", c.runtime.build, { mono: true }],
            ["Ayrıntı", c.runtime.rawDescription, { mono: true }],
          ]}
        />
      </Panel>,
    );
  }
  if (c?.browser?.name || userAgent?.browser) {
    panels.push(
      <Panel key="browser" title="Tarayıcı">
        <KVList
          items={[
            [
              "Tarayıcı",
              `${c?.browser?.name || userAgent?.browser} ${c?.browser?.version || userAgent?.browserVersion || ""}`.trim(),
            ],
            ["İşletim sistemi", userAgent?.os ? `${userAgent.os} ${userAgent.osVersion}`.trim() : undefined],
            ["Cihaz", [userAgent?.device, userAgent?.deviceType].filter(Boolean).join(", ")],
            ["Bot", userAgent?.bot ? "Evet" : undefined],
            ["User-Agent", userAgent?.raw || c?.browser?.userAgent, { mono: true }],
          ]}
        />
      </Panel>,
    );
  }
  if (c?.app && (c.app.name || c.app.version)) {
    panels.push(
      <Panel key="app" title="Uygulama">
        <KVList
          items={[
            ["Ad", c.app.name],
            ["Sürüm", c.app.build ? `${c.app.version} (${c.app.build})` : c.app.version, { mono: true }],
            ["Kimlik", c.app.id, { mono: true }],
            ["Başlangıç", c.app.startTime ? formatDateTime(c.app.startTime) : undefined],
            ["Ön planda", yesNo(c.app.inForeground)],
            ["Bellek", bytes(c.app.memoryBytes)],
          ]}
        />
      </Panel>,
    );
  }
  const p = c?.process;
  if (p && (p.pid || p.name || p.rssBytes > 0n)) {
    panels.push(
      <Panel key="process" title="Süreç">
        <div className="grid grid-cols-1 gap-3">
          {p.cpuPercent !== undefined ? <Meter label="Süreç işlemci kullanımı" value={p.cpuPercent} /> : null}
          <KVList
            items={[
              ["Süreç", [p.name, p.pid ? `pid ${p.pid}` : ""].filter(Boolean).join(", ")],
              ["Çalışma süresi", dur(p.uptime)],
              ["RSS", bytes(p.rssBytes)],
              [
                "Heap",
                p.heapAllocBytes > 0n
                  ? `${bytes(p.heapAllocBytes)} / ${bytes(p.heapSysBytes) ?? "?"}`
                  : undefined,
              ],
              ["İş parçacığı", count(p.threads)],
              ["Goroutine", count(p.goroutines)],
              ["Açık dosya", count(p.openFds)],
              [
                "GC",
                p.gcCount ? `${p.gcCount} kez, toplam ${dur(p.gcPauseTotal) ?? "0 ms"} duraklama` : undefined,
              ],
              [
                "İşlemci süresi",
                p.cpuTimeUser
                  ? `${dur(p.cpuTimeUser) ?? "0"} kullanıcı, ${dur(p.cpuTimeSystem) ?? "0"} sistem`
                  : undefined,
              ],
              ["Komut", p.commandLine, { mono: true }],
              ["Dizin", p.workingDirectory, { mono: true }],
            ]}
          />
        </div>
      </Panel>,
    );
  }
  const db = c?.database;
  if (db && (db.statement || db.system)) {
    panels.push(
      <Panel key="db" title="Veritabanı" className="@3xl:col-span-2">
        <div className="grid grid-cols-1 gap-3">
          <KVList
            items={[
              ["Sistem", [db.system, db.name].filter(Boolean).join(" / ")],
              ["Sunucu", db.host, { mono: true }],
              ["İşlem", db.operation],
              ["Süre", db.durationMs ? `${db.durationMs.toLocaleString("tr-TR")} ms` : undefined],
              ["Hata kodu", db.errorCode, { mono: true }],
            ]}
          />
          {db.statement ? <CodeBlock code={db.statement} lang="sql" maxHeight="16rem" /> : null}
          {db.parameters.length ? (
            <p className="font-mono text-[12px] text-muted-foreground">
              Parametreler: {db.parameters.map((v, i) => `$${i + 1}=${v}`).join("  ")}
            </p>
          ) : null}
        </div>
      </Panel>,
    );
  }
  if (c?.trace?.traceId) {
    panels.push(
      <Panel key="trace" title="İz">
        <KVList
          items={[
            ["Trace", c.trace.traceId, { mono: true }],
            ["Span", c.trace.spanId, { mono: true }],
            ["Üst span", c.trace.parentSpanId, { mono: true }],
            ["İşlem", c.trace.op],
            ["Durum", c.trace.status],
          ]}
        />
      </Panel>,
    );
  }
  if (c?.cloud?.provider || c?.cloud?.pod) {
    const cl = c.cloud;
    panels.push(
      <Panel key="cloud" title="Altyapı">
        <KVList
          items={[
            ["Sağlayıcı", [cl.provider, cl.platform].filter(Boolean).join(" / ")],
            ["Bölge", [cl.region, cl.availabilityZone].filter(Boolean).join(" / ")],
            ["Sunucu", cl.instanceId, { mono: true }],
            ["Küme", cl.cluster],
            ["Namespace", cl.namespace, { mono: true }],
            ["Pod", cl.pod, { mono: true }],
            ["Konteyner", cl.container, { mono: true }],
            ["Düğüm", cl.node, { mono: true }],
          ]}
        />
      </Panel>,
    );
  }
  if (c?.gpu?.name) {
    panels.push(
      <Panel key="gpu" title="Ekran kartı">
        <KVList
          items={[
            ["Ad", c.gpu.name],
            ["Üretici", c.gpu.vendorName],
            ["Bellek", bytes(c.gpu.memoryBytes)],
            ["API", c.gpu.apiType],
            ["Sürücü", c.gpu.driverVersion, { mono: true }],
          ]}
        />
      </Panel>,
    );
  }
  if (c?.culture?.locale || c?.culture?.timezone) {
    panels.push(
      <Panel key="culture" title="Dil ve bölge">
        <KVList
          items={[
            ["Yerel ayar", c.culture.locale, { mono: true }],
            ["Saat dilimi", c.culture.timezone, { mono: true }],
            ["Takvim", c.culture.calendar],
          ]}
        />
      </Panel>,
    );
  }
  for (const [name, value] of Object.entries(c?.custom ?? {})) {
    panels.push(
      <Panel key={`custom-${name}`} title={name}>
        <div className="font-mono text-[12px]">
          <JsonTree value={value} />
        </div>
      </Panel>,
    );
  }
  if (panels.length === 0) return null;
  return <div className="grid grid-cols-1 gap-3 @3xl:grid-cols-2">{panels}</div>;
}
