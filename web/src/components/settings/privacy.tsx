import { create } from "@bufbuild/protobuf";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { Field } from "@kentos/components/common/field";
import { StatStrip } from "@kentos/components/common/stat-strip";
import { Button } from "@kentos/components/ui/button";
import { Checkbox } from "@kentos/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Input } from "@kentos/components/ui/input";
import { Label } from "@kentos/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@kentos/components/ui/toggle-group";
import { Link } from "@tanstack/react-router";
import { DownloadIcon, SearchIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import {
  type DataSubject,
  DataSubjectSchema,
  type FindSubjectDataResponse,
  PrivacyService,
} from "@/gen/izci/api/v1/privacy_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorMessage } from "@/lib/errors";
import { formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";

type Kind = "person" | "customer";

/** Organization tab: find, export and delete what izci keeps about a person or a customer (KVKK). */
export function PrivacyRequests() {
  const org = useOrg();
  const transport = useTransport();
  const projects = useQuery(ProjectService.method.listProjects, {}, { staleTime: 60_000 });
  const [kind, setKind] = useState<Kind>("person");
  const [email, setEmail] = useState("");
  const [userId, setUserId] = useState("");
  const [username, setUsername] = useState("");
  const [ip, setIp] = useState("");
  const [customer, setCustomer] = useState("");
  const [project, setProject] = useState("*");
  // The subject of the last search: export and delete act on what was shown.
  const [subject, setSubject] = useState<DataSubject>();
  const [found, setFound] = useState<FindSubjectDataResponse>();
  const [busy, setBusy] = useState<"find" | "export" | "delete">();
  const [progress, setProgress] = useState(0);
  const [confirming, setConfirming] = useState(false);

  const find = async (s: DataSubject) => {
    setBusy("find");
    try {
      const res = await transport.unary(
        PrivacyService.method.findSubjectData,
        undefined,
        undefined,
        undefined,
        {
          subject: s,
        },
      );
      setSubject(s);
      setFound(res.message);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(undefined);
    }
  };

  const exportData = async () => {
    if (!subject) return;
    setBusy("export");
    setProgress(0);
    try {
      const events: unknown[] = [];
      let feedback: unknown[] = [];
      let pageToken = "";
      do {
        const res = await transport.unary(
          PrivacyService.method.exportSubjectData,
          undefined,
          undefined,
          undefined,
          {
            subject,
            pageSize: 500,
            pageToken,
          },
        );
        for (const e of res.message.eventsJson) events.push(JSON.parse(e));
        if (!pageToken) feedback = res.message.feedbackJson.map((f) => JSON.parse(f));
        pageToken = res.message.nextPageToken;
        setProgress(events.length);
      } while (pageToken);
      const file = {
        exported_at: new Date().toISOString(),
        organization: org.slug,
        subject: {
          email: subject.email || undefined,
          user_id: subject.userId || undefined,
          username: subject.username || undefined,
          ip_address: subject.ipAddress || undefined,
          customer: subject.customer || undefined,
          project: subject.project || undefined,
        },
        events,
        feedback,
      };
      const url = URL.createObjectURL(
        new Blob([JSON.stringify(file, null, 2)], { type: "application/json" }),
      );
      const a = document.createElement("a");
      a.href = url;
      a.download = `izci-kisisel-veri-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      URL.revokeObjectURL(url);
      toast.success(`${formatNumber(events.length)} olay dışa aktarıldı`);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(undefined);
    }
  };

  const deleteData = async () => {
    if (!subject) return;
    setBusy("delete");
    setProgress(0);
    try {
      let deleted = 0n;
      for (;;) {
        const res = await transport.unary(
          PrivacyService.method.deleteSubjectData,
          undefined,
          undefined,
          undefined,
          {
            subject,
          },
        );
        deleted += res.message.events;
        setProgress(Number(deleted));
        if (res.message.done) break;
      }
      setConfirming(false);
      toast.success(`${formatNumber(deleted)} olay ve bağlı veriler silindi`);
      await find(subject);
    } catch (err) {
      toast.error(errorMessage(err));
      setBusy(undefined);
    }
  };

  const person = kind === "person";
  const filled = person ? [email, userId, username, ip].some((v) => v.trim()) : customer.trim() !== "";

  return (
    <div className="grid max-w-4xl grid-cols-1 *:min-w-0 gap-6 py-6">
      <div className="max-w-2xl">
        <h2 className="font-semibold text-lg">Kişisel veri talepleri</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          KVKK ve GDPR kapsamındaki bilgi edinme ve silme taleplerinde bir kişinin ya da müşterinin İzci'deki
          olaylarını, eklerini ve geri bildirimlerini bulun, JSON olarak dışa aktarın ya da silin. Dışa
          aktarmalar ve silmeler denetim kaydına yazılır.
        </p>
      </div>
      <form
        className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          const s = create(DataSubjectSchema, {
            project: project === "*" ? "" : project,
            ...(person
              ? {
                  email: email.trim(),
                  userId: userId.trim(),
                  username: username.trim(),
                  ipAddress: ip.trim(),
                }
              : { customer: customer.trim() }),
          });
          void find(s);
        }}
      >
        <ToggleGroup
          type="single"
          variant="outline"
          value={kind}
          onValueChange={(v) => v && setKind(v as Kind)}
          aria-label="Talep kimin için"
        >
          <ToggleGroupItem value="person" className="px-4">
            Kişi
          </ToggleGroupItem>
          <ToggleGroupItem value="customer" className="px-4">
            Müşteri
          </ToggleGroupItem>
        </ToggleGroup>
        {person ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="E-posta" htmlFor="dsr-email">
                <Input id="dsr-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
              </Field>
              <Field label="Kullanıcı kimliği" htmlFor="dsr-id" hint="Uygulamanın gönderdiği user.id">
                <Input id="dsr-id" value={userId} onChange={(e) => setUserId(e.target.value)} />
              </Field>
              <Field label="Kullanıcı adı" htmlFor="dsr-username">
                <Input id="dsr-username" value={username} onChange={(e) => setUsername(e.target.value)} />
              </Field>
              <Field label="IP adresi" htmlFor="dsr-ip">
                <Input
                  id="dsr-ip"
                  value={ip}
                  onChange={(e) => setIp(e.target.value)}
                  className="font-mono text-sm"
                />
              </Field>
            </div>
            <p className="text-sm text-muted-foreground leading-relaxed">
              Kişiyi tanıtan bilgilerin hepsini yazın: bunlardan herhangi biri geçen olaylar kişiye sayılır.
              Bir IP adresi birden çok kişiye ait olabilir; yalnız gerekirse kullanın. Anonimleştirilmiş
              adresler .0 ile biter.
            </p>
          </>
        ) : (
          <Field
            label="Müşteri anahtarı"
            htmlFor="dsr-customer"
            hint="Olaylardaki customer.id, örneğin sivas-belediyesi. Müşterinin bütün olayları kapsanır."
          >
            <Input
              id="dsr-customer"
              value={customer}
              onChange={(e) => setCustomer(e.target.value)}
              className="font-mono text-sm"
            />
          </Field>
        )}
        <Field label="Proje" htmlFor="dsr-project">
          <Select value={project} onValueChange={setProject}>
            <SelectTrigger id="dsr-project" className="w-full sm:w-72">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="*">Bütün projeler</SelectItem>
              {projects.data?.projects.map((p) => (
                <SelectItem key={p.id} value={p.slug}>
                  {p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <div>
          <Button type="submit" disabled={!filled || busy !== undefined}>
            <SearchIcon className="size-4" />
            {busy === "find" ? "Aranıyor…" : "Verileri bul"}
          </Button>
        </div>
      </form>

      {found && subject ? (
        <section className="grid grid-cols-1 *:min-w-0 gap-4 border-t pt-6">
          <StatStrip
            stats={[
              { label: "Olay", value: formatNumber(found.events) },
              { label: "Hata", value: formatNumber(found.issues) },
              { label: "Ek", value: formatNumber(found.attachments) },
              { label: "Geri bildirim", value: formatNumber(found.feedback) },
            ]}
          />
          {found.latest.length > 0 ? (
            <div className="overflow-hidden rounded-lg border">
              <p className="border-b bg-(--surface-muted) px-4 py-2 font-medium text-muted-foreground text-xs">
                Son olaylar
              </p>
              <ul className="divide-y">
                {found.latest.map((e) => (
                  <li
                    key={e.id}
                    className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-4 py-2.5 text-sm"
                  >
                    <Link
                      to="/o/$org/issues/$issueId"
                      params={{ org: org.slug, issueId: e.issueShortId || e.id }}
                      search={{ event: e.id }}
                      className="font-mono text-link text-xs underline-offset-4 hover:underline"
                    >
                      {e.issueShortId}
                    </Link>
                    <span className="min-w-0 flex-1 truncate">{e.title}</span>
                    <span className="text-muted-foreground text-xs">{e.userLabel}</span>
                    <RelativeTime value={e.receivedAt} className="text-muted-foreground text-xs" />
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {found.events > 0n || found.feedback > 0n ? (
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" disabled={busy !== undefined} onClick={() => void exportData()}>
                <DownloadIcon className="size-4" />
                {busy === "export"
                  ? `Dışa aktarılıyor… ${formatNumber(progress)} olay`
                  : "JSON olarak dışa aktar"}
              </Button>
              <Button variant="destructive" disabled={busy !== undefined} onClick={() => setConfirming(true)}>
                <Trash2Icon className="size-4" />
                Verileri sil
              </Button>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">Bu bilgilerle eşleşen bir kayıt yok.</p>
          )}
        </section>
      ) : null}

      {found ? (
        <DeleteSubjectDialog
          found={found}
          open={confirming}
          onOpenChange={(v) => busy !== "delete" && setConfirming(v)}
          deleting={busy === "delete"}
          progress={progress}
          onConfirm={() => void deleteData()}
        />
      ) : null}
    </div>
  );
}

function DeleteSubjectDialog({
  found,
  open,
  onOpenChange,
  deleting,
  progress,
  onConfirm,
}: {
  found: FindSubjectDataResponse;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  deleting: boolean;
  progress: number;
  onConfirm: () => void;
}) {
  const [sure, setSure] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) setSure(false);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Veriler kalıcı olarak silinsin mi?</DialogTitle>
          <DialogDescription>
            {formatNumber(found.events)} olay, ekleri ve {formatNumber(found.feedback)} geri bildirim silinir;
            hatalardaki etkilenen kullanıcı ve etiket kayıtları da kaldırılır. Hatalar ve sayaçları kalır. Bu
            işlem geri alınamaz.
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-start gap-2.5">
          <Checkbox
            id="dsr-sure"
            checked={sure}
            onCheckedChange={(v) => setSure(v === true)}
            className="mt-0.5"
          />
          <Label htmlFor="dsr-sure" className="font-normal text-sm leading-relaxed">
            Silmeden önce gerekiyorsa verileri dışa aktardım; silinen veriler geri getirilemez.
          </Label>
        </div>
        <DialogFooter>
          <Button variant="ghost" disabled={deleting} onClick={() => onOpenChange(false)}>
            Vazgeç
          </Button>
          <Button variant="destructive" disabled={!sure || deleting} onClick={onConfirm}>
            {deleting ? `Siliniyor… ${formatNumber(progress)} olay` : "Kalıcı olarak sil"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
