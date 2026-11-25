import { createConnectQueryKey, useMutation, useQuery, useTransport } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { BookmarkIcon, DownloadIcon, Trash2Icon, UsersIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/common/field";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { IssueStatus, type StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { IssueService, type IssueSort } from "@/gen/izci/api/v1/issue_pb";
import { type SavedSearch, ViewService } from "@/gen/izci/api/v1/view_pb";
import { downloadText, toCsv } from "@/lib/csv";
import { errorMessage } from "@/lib/errors";
import { formatDateTime } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { canWrite } from "@/lib/session";

/** The search on screen. */
export type CurrentSearch = { query: string; sort: IssueSort; period: StatsPeriod; projects: string[] };

/** Saved searches of the issue list: apply one, save the current search, delete. */
export function SavedSearchMenu({
  current,
  onApply,
}: {
  current: CurrentSearch;
  onApply: (s: SavedSearch) => void;
}) {
  const org = useOrg();
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);
  const list = useQuery(ViewService.method.listSavedSearches, {}, { staleTime: 60_000 });
  const invalidate = () =>
    qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: ViewService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
  const remove = useMutation(ViewService.method.deleteSavedSearch, {
    onSuccess: () => {
      toast.success("Kayıtlı arama silindi");
      void invalidate();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const searches = list.data?.searches ?? [];
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" variant="outline">
            <BookmarkIcon className="size-4" />
            Kayıtlı aramalar
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-72">
          {searches.length === 0 ? (
            <DropdownMenuLabel className="font-normal text-muted-foreground text-sm">
              Henüz kayıtlı arama yok.
            </DropdownMenuLabel>
          ) : (
            searches.map((s) => (
              <DropdownMenuItem key={s.id} onSelect={() => onApply(s)} className="gap-2">
                <span className="min-w-0 flex-1 truncate">{s.name}</span>
                {s.shared ? (
                  <UsersIcon className="size-3.5 text-muted-foreground" aria-label="Ortak" />
                ) : null}
                {s.editable ? (
                  <button
                    type="button"
                    className="rounded p-0.5 text-muted-foreground hover:text-destructive"
                    aria-label={`${s.name} aramasını sil`}
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      remove.mutate({ id: s.id });
                    }}
                  >
                    <Trash2Icon className="size-3.5" />
                  </button>
                ) : null}
              </DropdownMenuItem>
            ))
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setSaving(true)}>Bu aramayı kaydet…</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <SaveSearchDialog
        current={current}
        open={saving}
        onOpenChange={setSaving}
        onSaved={() => void invalidate()}
      />
    </>
  );
}

function SaveSearchDialog({
  current,
  open,
  onOpenChange,
  onSaved,
}: {
  current: CurrentSearch;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const org = useOrg();
  const [name, setName] = useState("");
  const [shared, setShared] = useState(false);
  const save = useMutation(ViewService.method.saveSearch, {
    onSuccess: () => {
      toast.success("Arama kaydedildi");
      setName("");
      onOpenChange(false);
      onSaved();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Aramayı kaydet</DialogTitle>
          <DialogDescription>
            Arama ifadesi, projeler, sıralama ve dönem saklanır:{" "}
            <code className="font-mono text-foreground text-xs">{current.query}</code>
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate({
              search: {
                name,
                query: current.query,
                sort: current.sort,
                statsPeriod: current.period,
                projects: current.projects,
                shared,
              },
            });
          }}
        >
          <Field label="Ad" htmlFor="search-name">
            <Input
              id="search-name"
              required
              maxLength={100}
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
          </Field>
          {canWrite(org.role) ? (
            <div className="flex items-start gap-2.5">
              <Checkbox
                id="search-shared"
                checked={shared}
                onCheckedChange={(v) => setShared(v === true)}
                className="mt-0.5"
              />
              <Label htmlFor="search-shared" className="grid gap-0.5 font-normal text-sm">
                <span>Organizasyonla paylaş</span>
                <span className="text-muted-foreground text-xs">
                  Bütün üyeler görür; yalnız siz ve yöneticiler değiştirir.
                </span>
              </Label>
            </div>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={save.isPending || !name.trim()}>
              Kaydet
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

const statusText: Record<number, string> = {
  [IssueStatus.UNRESOLVED]: "Açık",
  [IssueStatus.RESOLVED]: "Çözüldü",
  [IssueStatus.IGNORED]: "Yoksayıldı",
};

/** Downloads up to 1000 issues of the search as CSV. */
export function ExportCsvButton({ current }: { current: CurrentSearch }) {
  const transport = useTransport();
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    try {
      const rows: (string | number | bigint)[][] = [];
      let pageToken = "";
      do {
        const res = await transport.unary(IssueService.method.listIssues, undefined, undefined, undefined, {
          query: current.query,
          sort: current.sort,
          statsPeriod: current.period,
          projects: current.projects,
          pageSize: 100,
          pageToken,
        });
        for (const i of res.message.issues) {
          rows.push([
            i.shortId,
            i.title,
            i.project?.name ?? "",
            statusText[i.status] ?? "",
            i.periodCount,
            i.timesSeen,
            i.usersCount,
            i.customersCount,
            i.assignee?.email ?? "",
            formatDateTime(i.firstSeen),
            formatDateTime(i.lastSeen),
            i.permalink,
          ]);
        }
        pageToken = res.message.nextPageToken;
      } while (pageToken && rows.length < 1000);
      const head = [
        "Kısa kimlik",
        "Başlık",
        "Proje",
        "Durum",
        "Dönemdeki olay",
        "Toplam olay",
        "Kullanıcı",
        "Müşteri",
        "Atanan",
        "İlk görülme",
        "Son görülme",
        "Bağlantı",
      ];
      downloadText(
        `izci-hatalar-${new Date().toISOString().slice(0, 10)}.csv`,
        toCsv(head, rows.slice(0, 1000)),
      );
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button
      size="sm"
      variant="outline"
      disabled={busy}
      onClick={() => void run()}
      aria-label="CSV olarak indir"
    >
      <DownloadIcon className="size-4" />
      CSV
    </Button>
  );
}
