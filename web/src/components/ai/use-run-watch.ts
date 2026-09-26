import { createClient } from "@connectrpc/connect";
import { createConnectQueryKey } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { type AiLogEntry, type AiRun, AiService } from "@/gen/izci/api/v1/ai_pb";
import { IssueService } from "@/gen/izci/api/v1/issue_pb";
import { useOrg } from "@/lib/org";
import { isActive } from "./labels";

/** Refreshes cached AI runs and the issue after a run changed. */
export function useInvalidateRuns() {
  const org = useOrg();
  const qc = useQueryClient();
  return () =>
    Promise.all(
      [AiService, IssueService].map((schema) =>
        qc.invalidateQueries({
          queryKey: createConnectQueryKey({ schema, transport: org.transport, cardinality: undefined }),
        }),
      ),
    );
}

/**
 * Follows a queued or running run: the stream replays the recorded log,
 * then delivers new entries and status changes until the run finishes.
 * Reconnects with backoff when the connection drops.
 */
export function useRunWatch(run: AiRun | undefined) {
  const org = useOrg();
  const invalidate = useInvalidateRuns();
  const [live, setLive] = useState<{ id: string; run?: AiRun; log: AiLogEntry[]; connected: boolean }>({
    id: "",
    log: [],
    connected: false,
  });
  const id = run?.id ?? "";
  const active = isActive(run);

  // biome-ignore lint/correctness/useExhaustiveDependencies: restart only when the run or its activity changes
  useEffect(() => {
    if (!id || !active) return;
    const client = createClient(AiService, org.transport);
    const abort = new AbortController();
    (async () => {
      for (let attempt = 0; !abort.signal.aborted; attempt++) {
        let log: AiLogEntry[] = [];
        setLive({ id, log, connected: false });
        try {
          for await (const msg of client.watchRun({ id }, { signal: abort.signal })) {
            if (msg.update.case === "log") {
              log = [...log, msg.update.value];
              setLive((s) => ({ ...s, log, connected: true }));
            } else if (msg.update.case === "run") {
              const next = msg.update.value;
              setLive((s) => {
                // Lists and the issue show the status too.
                if (s.run && s.run.status !== next.status) void invalidate();
                return { ...s, run: next, connected: true };
              });
              if (!isActive(next)) {
                void invalidate();
                return;
              }
            }
          }
          return;
        } catch {
          // Reconnect below unless the component went away.
        }
        if (abort.signal.aborted) return;
        await new Promise((r) => setTimeout(r, Math.min(1000 * 2 ** attempt, 10_000)));
      }
    })();
    return () => abort.abort();
  }, [id, active, org.transport]);

  return live.id === id ? live : { id, log: [] as AiLogEntry[], connected: false, run: undefined };
}
