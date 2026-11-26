import { skipToken, useMutation, useQuery } from "@connectrpc/connect-query";
import { Field, FormError } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from "@kentos/components/ui/command";
import { Input } from "@kentos/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@kentos/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Link } from "@tanstack/react-router";
import { ListIcon, LockIcon, MinusIcon, PlusIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { IntegrationAuth, IntegrationService } from "@/gen/izci/api/v1/integration_pb";
import { type Project, ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorField, errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { useInvalidateProjects } from "./use-project";

type Mapping = { from: string; to: string };

const noIntegration = "none";

/** Repositories of a GitHub connection, searchable; picks fill the form. */
function RepositoryPicker({
  integrationId,
  onPick,
}: {
  integrationId: string;
  onPick: (fullName: string, branch: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebounced(query), 250);
    return () => clearTimeout(t);
  }, [query]);
  const repos = useQuery(
    IntegrationService.method.listRepositories,
    open ? { integrationId, query: debounced } : skipToken,
  );
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="icon" aria-label="Depolardan seç">
          <ListIcon className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-0">
        <Command shouldFilter={false}>
          <CommandInput value={query} onValueChange={setQuery} placeholder="Depo ara…" />
          <CommandList>
            <CommandEmpty>
              {repos.isFetching ? "Aranıyor…" : repos.error ? "Depolar alınamadı." : "Depo bulunamadı."}
            </CommandEmpty>
            {repos.data?.repositories.map((r) => (
              <CommandItem
                key={r.fullName}
                value={r.fullName}
                onSelect={() => {
                  onPick(r.fullName, r.defaultBranch);
                  setOpen(false);
                }}
                className="flex items-center gap-2"
              >
                <span className="min-w-0 flex-1 truncate font-mono text-sm">{r.fullName}</span>
                {r.private ? (
                  <LockIcon className="size-3.5 text-muted-foreground" aria-label="Özel depo" />
                ) : null}
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export function ProjectRepository({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const invalidate = useInvalidateProjects();
  const repo = project.repository;
  const [repository, setRepository] = useState(repo ? `${repo.owner}/${repo.name}` : "");
  const [branch, setBranch] = useState(repo?.defaultBranch ?? "");
  const [integrationId, setIntegrationId] = useState(repo?.integrationId || noIntegration);
  const integrations = useQuery(IntegrationService.method.listIntegrations, {}, { enabled: admin });
  const connected = integrations.data?.integrations ?? [];
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
    <div className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-6 py-6">
      <div>
        <h2 className="font-semibold text-lg">Kaynak kod deposu</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
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
              className="font-medium font-mono text-sm hover:underline"
            >
              {repo.owner}/{repo.name}
            </a>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {repo.defaultBranch} dalı, <RelativeTime value={repo.linkedAt} /> bağlandı
              {repo.integrationId ? "" : " · GitHub bağlantısı yok, yapay zekâ yalnız analiz yapar"}
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
              integrationId: integrationId === noIntegration ? "" : integrationId,
              defaultBranch: branch,
              pathMappings: mappings
                .filter((m) => m.from.trim())
                .map((m) => ({ fromPrefix: m.from.trim(), toPrefix: m.to.trim() })),
            });
          }}
        >
          <Field
            label="GitHub bağlantısı"
            htmlFor="repo-integration"
            hint={
              connected.length === 0 ? (
                <>
                  Yapay zekânın kodu okuyup düzeltme göndermesi için önce{" "}
                  <Link
                    to="/o/$org/settings"
                    params={{ org: org.slug }}
                    search={{ tab: "integrations" }}
                    className="text-link hover:underline"
                  >
                    bir GitHub hesabı bağlayın
                  </Link>
                  .
                </>
              ) : (
                "Yapay zekâ depoyu bu bağlantıyla okur ve düzeltmeleri bu bağlantıyla gönderir."
              )
            }
          >
            <Select value={integrationId} onValueChange={setIntegrationId}>
              <SelectTrigger id="repo-integration" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={noIntegration}>Bağlantı yok (yalnız kaynak bağlantıları)</SelectItem>
                {connected.map((i) => (
                  <SelectItem key={i.id} value={i.id}>
                    {i.accountLogin || i.name} ·{" "}
                    {i.auth === IntegrationAuth.APP ? "GitHub App" : "erişim anahtarı"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field
            label="Depo"
            htmlFor="repo"
            error={
              link.error && errorField(link.error) === "repository" ? errorMessage(link.error) : undefined
            }
            hint="sahip/depo biçiminde ya da GitHub adresi olarak."
          >
            <div className="flex gap-2">
              <Input
                id="repo"
                required
                value={repository}
                onChange={(e) => setRepository(e.target.value)}
                placeholder="kentos/kalem"
                className="font-mono text-sm"
              />
              {integrationId !== noIntegration ? (
                <RepositoryPicker
                  integrationId={integrationId}
                  onPick={(fullName, defaultBranch) => {
                    setRepository(fullName);
                    setBranch(defaultBranch);
                  }}
                />
              ) : null}
            </div>
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
              className="w-60 font-mono text-sm"
            />
          </Field>
          <div className="grid gap-2">
            <p className="font-medium text-sm">Yol eşlemeleri</p>
            <p className="-mt-1 text-xs text-muted-foreground">
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
                  className="font-mono text-sm"
                />
                <span className="text-muted-foreground">→</span>
                <Input
                  aria-label="Depodaki önek"
                  value={m.to}
                  onChange={(e) => setMapping(i, { to: e.target.value })}
                  placeholder="src/"
                  className="font-mono text-sm"
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
