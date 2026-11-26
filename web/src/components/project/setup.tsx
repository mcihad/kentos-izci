import { useMutation, useQuery } from "@connectrpc/connect-query";
import { SecretValue } from "@kentos/components/common/copy-button";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { Link, useNavigate } from "@tanstack/react-router";
import { SendIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { CodeBlock } from "@/components/common/code-block";
import { type Project, ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canWrite } from "@/lib/session";
import { setupSnippets } from "./snippets";

export function ProjectSetup({ project }: { project: Project }) {
  const org = useOrg();
  const navigate = useNavigate();
  const ep = useQuery(ProjectService.method.getIngestEndpoints, { project: project.slug });
  const [active, setActive] = useState(() =>
    project.platform === "go" ? "go" : project.platform === "dotnet" ? "dotnet" : "curl",
  );
  const test = useMutation(ProjectService.method.sendTestEvent, {
    onSuccess: (res) =>
      toast.success("Test olayı alındı", {
        description: res.issueShortId ? `${res.issueShortId} hatası altında gruplandı.` : undefined,
        action: res.issueShortId
          ? {
              label: "Aç",
              onClick: () =>
                void navigate({
                  to: "/o/$org/issues/$issueId",
                  params: { org: org.slug, issueId: res.issueShortId },
                }),
            }
          : undefined,
      }),
    onError: (err) => toast.error(errorMessage(err)),
  });
  if (!ep.data) return <Skeleton className="my-6 h-72 w-full" />;
  const snippets = setupSnippets(ep.data);
  const current = snippets.find((s) => s.id === active) ?? snippets[0];

  return (
    <div className="grid max-w-4xl grid-cols-1 *:min-w-0 gap-8 py-6">
      <section className="grid grid-cols-1 gap-3">
        <h2 className="font-semibold text-lg">Bağlantı bilgileri</h2>
        <dl className="grid grid-cols-1 gap-3 sm:grid-cols-[10rem_minmax(0,1fr)] sm:items-center sm:gap-x-5">
          <dt className="text-sm text-muted-foreground">Adres</dt>
          <dd>
            <SecretValue value={ep.data.baseUrl} />
          </dd>
          <dt className="text-sm text-muted-foreground">gRPC hedefi</dt>
          <dd>
            <SecretValue value={ep.data.grpcTarget} />
          </dd>
          <dt className="text-sm text-muted-foreground">Sentry DSN</dt>
          <dd>
            <SecretValue value={ep.data.sentryDsnTemplate.replace("<key>", "<anahtar>")} />
          </dd>
        </dl>
        <p className="text-sm text-muted-foreground">
          Kodlarda geçen <code className="font-mono">&lt;anahtar&gt;</code> ve{" "}
          <code className="font-mono">IZCI_KEY</code> yerine{" "}
          <Link
            from="/o/$org/projects/$slug"
            search={{ tab: "keys" }}
            className="text-foreground underline underline-offset-4"
          >
            Anahtarlar
          </Link>{" "}
          sekmesinde oluşturduğunuz, <code className="font-mono">izk_</code> ile başlayan anahtarı kullanın.
        </p>
      </section>

      <section className="grid grid-cols-1 gap-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <h2 className="font-semibold text-lg">Uygulamanıza ekleyin</h2>
          {canWrite(org.role) ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => test.mutate({ project: project.slug })}
              disabled={test.isPending}
            >
              <SendIcon className="size-4" />
              {test.isPending ? "Gönderiliyor…" : "Test olayı gönder"}
            </Button>
          ) : null}
        </div>
        <div
          className="scrollbar-none flex gap-1 overflow-x-auto border-b max-sm:fade-end max-sm:pr-8"
          role="tablist"
          aria-label="Dil"
        >
          {snippets.map((s) => (
            <button
              key={s.id}
              type="button"
              role="tab"
              aria-selected={s.id === current?.id}
              onClick={() => setActive(s.id)}
              className={cn(
                "-mb-px min-h-11 whitespace-nowrap border-b-2 px-3 text-md transition-colors sm:min-h-10",
                s.id === current?.id
                  ? "border-primary font-semibold text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {s.label}
            </button>
          ))}
        </div>
        {current ? (
          <>
            <p className="text-md text-muted-foreground">{current.intro}</p>
            <CodeBlock code={current.code} lang={current.lang} maxHeight="34rem" />
          </>
        ) : null}
        <p className="text-sm text-muted-foreground">
          Küçültülmüş JavaScript ya da karıştırılmış Flutter kodunun yığın izlerini okunur yapmak için{" "}
          <Link
            from="/o/$org/projects/$slug"
            search={{ tab: "symbols" }}
            className="text-foreground underline underline-offset-4"
          >
            Kaynak eşlemeleri
          </Link>{" "}
          sekmesine bakın.
        </p>
      </section>
    </div>
  );
}
