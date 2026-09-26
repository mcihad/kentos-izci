import { useMutation } from "@connectrpc/connect-query";
import { MinusIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Field, FormError } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { type Project, ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorField, errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { useInvalidateProjects } from "./use-project";

type Mapping = { from: string; to: string };

export function ProjectRepository({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const invalidate = useInvalidateProjects();
  const repo = project.repository;
  const [repository, setRepository] = useState(repo ? `${repo.owner}/${repo.name}` : "");
  const [branch, setBranch] = useState(repo?.defaultBranch ?? "");
  const [mappings, setMappings] = useState<Mapping[]>(
    repo?.pathMappings.length
      ? repo.pathMappings.map((m) => ({ from: m.fromPrefix, to: m.toPrefix }))
      : [{ from: "", to: "" }],
  );
  const link = useMutation(ProjectService.method.linkRepository, {
    onSuccess: () => {
      toast.success("Depo bağlandı");
      void invalidate();
    },
  });
  const unlink = useMutation(ProjectService.method.unlinkRepository, {
    onSuccess: () => {
      toast.success("Depo bağlantısı kaldırıldı");
      setRepository("");
      void invalidate();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const setMapping = (i: number, patch: Partial<Mapping>) =>
    setMappings((m) => m.map((x, j) => (j === i ? { ...x, ...patch } : x)));

  return (
    <div className="grid max-w-2xl grid-cols-1 gap-6 py-6">
      <div>
        <h2 className="font-semibold text-[15px]">Kaynak kod deposu</h2>
        <p className="mt-0.5 text-[13px] text-muted-foreground leading-relaxed">
          Depo bağlandığında yığın izindeki satırlar doğrudan GitHub'daki koda bağlanır. Yapay zekâ analizi
          için ayrıca bir GitHub entegrasyonu gerekir.
        </p>
      </div>
      {repo ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
          <div className="min-w-0">
            <a
              href={repo.url}
              target="_blank"
              rel="noreferrer"
              className="font-medium font-mono text-[13px] hover:underline"
            >
              {repo.owner}/{repo.name}
            </a>
            <p className="mt-0.5 text-[12px] text-muted-foreground">
              {repo.defaultBranch} dalı, <RelativeTime value={repo.linkedAt} /> bağlandı
            </p>
          </div>
          {admin ? (
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive"
              disabled={unlink.isPending}
              onClick={() => unlink.mutate({ project: project.slug })}
            >
              Bağlantıyı kaldır
            </Button>
          ) : null}
        </div>
      ) : null}
      {admin ? (
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            link.mutate({
              project: project.slug,
              repository,
              defaultBranch: branch,
              pathMappings: mappings
                .filter((m) => m.from.trim())
                .map((m) => ({ fromPrefix: m.from.trim(), toPrefix: m.to.trim() })),
            });
          }}
        >
          <Field
            label="Depo"
            htmlFor="repo"
            error={
              link.error && errorField(link.error) === "repository" ? errorMessage(link.error) : undefined
            }
            hint="sahip/depo biçiminde ya da GitHub adresi olarak."
          >
            <Input
              id="repo"
              required
              value={repository}
              onChange={(e) => setRepository(e.target.value)}
              placeholder="kentos/kalem"
              className="font-mono text-[13px]"
            />
          </Field>
          <Field
            label="Varsayılan dal"
            htmlFor="branch"
            hint="Sürüm için commit bilinmediğinde bağlantılar bu dala gider."
          >
            <Input
              id="branch"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder="main"
              className="w-60 font-mono text-[13px]"
            />
          </Field>
          <div className="grid gap-2">
            <p className="font-medium text-[13px]">Yol eşlemeleri</p>
            <p className="-mt-1 text-[12px] text-muted-foreground">
              Uygulamanın bildirdiği dosya yolunu depodaki yola çevirir, örneğin{" "}
              <code className="font-mono">/app/</code> → <code className="font-mono">src/</code>
            </p>
            {mappings.map((m, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: rows are edited in place
              <div key={i} className="flex items-center gap-2">
                <Input
                  aria-label="Uygulamadaki önek"
                  value={m.from}
                  onChange={(e) => setMapping(i, { from: e.target.value })}
                  placeholder="/app/"
                  className="font-mono text-[12.5px]"
                />
                <span className="text-muted-foreground">→</span>
                <Input
                  aria-label="Depodaki önek"
                  value={m.to}
                  onChange={(e) => setMapping(i, { to: e.target.value })}
                  placeholder="src/"
                  className="font-mono text-[12.5px]"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label="Eşlemeyi kaldır"
                  onClick={() =>
                    setMappings((x) => (x.length > 1 ? x.filter((_, j) => j !== i) : [{ from: "", to: "" }]))
                  }
                >
                  <MinusIcon className="size-4" />
                </Button>
              </div>
            ))}
            <div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setMappings((x) => [...x, { from: "", to: "" }])}
              >
                <PlusIcon className="size-4" />
                Eşleme ekle
              </Button>
            </div>
          </div>
          <FormError
            message={
              link.error && errorField(link.error) !== "repository" ? errorMessage(link.error) : undefined
            }
          />
          <div>
            <Button type="submit" disabled={link.isPending}>
              {repo ? "Güncelle" : "Depoyu bağla"}
            </Button>
          </div>
        </form>
      ) : null}
    </div>
  );
}
