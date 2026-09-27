import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { CodeBlock } from "@/components/common/code-block";
import { SecretValue } from "@/components/common/copy-button";
import { Field, FormError } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { type CreateAccessTokenResponse, TokenScope, UserService } from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { useSession } from "@/lib/session";

const scopeLabels: Record<TokenScope, string> = {
  [TokenScope.UNSPECIFIED]: "",
  [TokenScope.READ]: "Okuma",
  [TokenScope.WRITE]: "Yazma",
  [TokenScope.AI]: "Yapay zekâ",
};

const expiries = [
  { value: "30", label: "30 gün" },
  { value: "90", label: "90 gün" },
  { value: "365", label: "1 yıl" },
  { value: "0", label: "Süresiz" },
];

export function Tokens() {
  const qc = useQueryClient();
  const list = useQuery(UserService.method.listAccessTokens, {}, { transport: baseTransport });
  const [creating, setCreating] = useState(false);
  const revoke = useMutation(UserService.method.revokeAccessToken, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Erişim anahtarı iptal edildi");
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: UserService.method.listAccessTokens,
          transport: baseTransport,
          cardinality: "finite",
        }),
      });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <div className="max-w-3xl py-6">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="font-semibold text-lg">Kişisel erişim anahtarları</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            MCP istemcileri (Claude Code, opencode, Cursor), betikler ve API için. Anahtar sizin
            yetkilerinizle çalışır.
          </p>
        </div>
        <Button size="sm" onClick={() => setCreating(true)}>
          <PlusIcon className="size-4" />
          Anahtar oluştur
        </Button>
      </div>
      {!list.data ? (
        <Skeleton className="h-32 w-full" />
      ) : list.data.tokens.length === 0 ? (
        <EmptyState icon={KeyRoundIcon} title="Henüz erişim anahtarınız yok" className="rounded-lg border" />
      ) : (
        <ul className="divide-y rounded-lg border">
          {list.data.tokens.map((t) => (
            <li key={t.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="font-medium text-md">{t.name}</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  <span className="font-mono">{t.prefix}…</span>,{" "}
                  {t.organization?.name ?? "tüm organizasyonlar"},{" "}
                  {t.scopes.map((s) => scopeLabels[s]).join(" + ")},{" "}
                  {t.lastUsedAt ? (
                    <>
                      son kullanım <RelativeTime value={t.lastUsedAt} />
                    </>
                  ) : (
                    "hiç kullanılmadı"
                  )}
                  {t.expiresAt ? (
                    <>
                      , <RelativeTime value={t.expiresAt} /> sona eriyor
                    </>
                  ) : null}
                </p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="text-destructive"
                disabled={revoke.isPending}
                onClick={() => revoke.mutate({ id: t.id })}
              >
                İptal et
              </Button>
            </li>
          ))}
        </ul>
      )}
      <CreateTokenDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}

function CreateTokenDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const org = useOrg();
  const session = useSession();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [orgSlug, setOrgSlug] = useState(org.slug);
  const [write, setWrite] = useState(false);
  const [ai, setAi] = useState(false);
  const [expiry, setExpiry] = useState("90");
  const [created, setCreated] = useState<CreateAccessTokenResponse>();
  const createToken = useMutation(UserService.method.createAccessToken, {
    transport: baseTransport,
    onSuccess: (res) => {
      setCreated(res);
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: UserService.method.listAccessTokens,
          transport: baseTransport,
          cardinality: "finite",
        }),
      });
    },
  });
  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v) {
      setCreated(undefined);
      setName("");
      createToken.reset();
    }
  };
  const mcpUrl = `${window.location.origin}/mcp`;
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-xl">
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Erişim anahtarı oluşturuldu</DialogTitle>
              <DialogDescription>
                Anahtar yalnızca şimdi gösterilir. MCP istemcinize eklemek için komutu kullanabilirsiniz.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-4">
              <SecretValue value={created.secret} />
              <div className="grid gap-1.5">
                <p className="font-medium text-sm">Claude Code</p>
                <CodeBlock
                  lang="shellscript"
                  code={`claude mcp add --transport http izci ${mcpUrl} \\\n  --header "Authorization: Bearer ${created.secret}"`}
                />
              </div>
              <div className="grid gap-1.5">
                <p className="font-medium text-sm">opencode (opencode.json)</p>
                <CodeBlock
                  lang="json"
                  code={JSON.stringify(
                    {
                      mcp: {
                        izci: {
                          type: "remote",
                          url: mcpUrl,
                          headers: { Authorization: `Bearer ${created.secret}` },
                        },
                      },
                    },
                    null,
                    2,
                  )}
                />
              </div>
              <div className="grid gap-1.5">
                <p className="font-medium text-sm">Cursor (.cursor/mcp.json)</p>
                <CodeBlock
                  lang="json"
                  code={JSON.stringify(
                    {
                      mcpServers: {
                        izci: { url: mcpUrl, headers: { Authorization: `Bearer ${created.secret}` } },
                      },
                    },
                    null,
                    2,
                  )}
                />
              </div>
              <div className="grid gap-1.5">
                <p className="font-medium text-sm">Yerel süreç olarak (stdio)</p>
                <CodeBlock
                  lang="shellscript"
                  code={`IZCI_TOKEN=${created.secret} izci mcp --url ${window.location.origin}`}
                />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={() => close(false)}>Kaydettim, kapat</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Yeni erişim anahtarı</DialogTitle>
              <DialogDescription>Anahtara yalnızca ihtiyaç duyduğu yetkileri verin.</DialogDescription>
            </DialogHeader>
            <form
              className="grid gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                const scopes = [TokenScope.READ];
                if (write) scopes.push(TokenScope.WRITE);
                if (ai) scopes.push(TokenScope.AI);
                const days = Number(expiry);
                createToken.mutate({
                  name,
                  organization: orgSlug,
                  scopes,
                  expiresIn:
                    days > 0 ? create(DurationSchema, { seconds: BigInt(days * 86_400) }) : undefined,
                });
              }}
            >
              <Field label="Ad" htmlFor="tok-name" hint="Örneğin: Claude Code, dizüstü">
                <Input
                  id="tok-name"
                  required
                  autoFocus
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </Field>
              <Field label="Organizasyon" htmlFor="tok-org">
                <Select value={orgSlug} onValueChange={setOrgSlug}>
                  <SelectTrigger id="tok-org" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {session?.memberships.map((m) => (
                      <SelectItem key={m.organization?.id} value={m.organization?.slug ?? ""}>
                        {m.organization?.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <fieldset className="grid gap-2">
                <legend className="mb-1 font-medium text-sm">Yetkiler</legend>
                <div className="flex items-center gap-2">
                  <Checkbox id="tok-read" checked disabled />
                  <Label htmlFor="tok-read" className="font-normal text-sm">
                    Okuma: hataları, olayları ve müşterileri görme
                  </Label>
                </div>
                <div className="flex items-center gap-2">
                  <Checkbox id="tok-write" checked={write} onCheckedChange={(v) => setWrite(v === true)} />
                  <Label htmlFor="tok-write" className="font-normal text-sm">
                    Yazma: hataları çözme, atama, yorum
                  </Label>
                </div>
                <div className="flex items-center gap-2">
                  <Checkbox id="tok-ai" checked={ai} onCheckedChange={(v) => setAi(v === true)} />
                  <Label htmlFor="tok-ai" className="font-normal text-sm">
                    Yapay zekâ: analiz başlatma ve çekme isteği açma
                  </Label>
                </div>
              </fieldset>
              <Field label="Geçerlilik" htmlFor="tok-exp">
                <Select value={expiry} onValueChange={setExpiry}>
                  <SelectTrigger id="tok-exp" className="w-40">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {expiries.map((e) => (
                      <SelectItem key={e.value} value={e.value}>
                        {e.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <FormError message={createToken.error ? errorMessage(createToken.error) : undefined} />
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => close(false)}>
                  Vazgeç
                </Button>
                <Button type="submit" disabled={createToken.isPending}>
                  Anahtarı oluştur
                </Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
