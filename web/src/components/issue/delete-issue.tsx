import { createConnectQueryKey, useMutation } from "@connectrpc/connect-query";
import { ConfirmDialog } from "@kentos/components/common/dialogs";
import { Button } from "@kentos/components/ui/button";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { EventService } from "@/gen/izci/api/v1/event_pb";
import { type Issue, IssueService } from "@/gen/izci/api/v1/issue_pb";
import { failWith } from "@/lib/errors";
import { useOrg } from "@/lib/org";

/** Deletes an issue with its events, after a confirmation (administrators). */
export function DeleteIssueButton({ issue }: { issue: Issue }) {
  const org = useOrg();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const s = issue.summary;
  const del = useMutation(IssueService.method.deleteIssues, {
    onSuccess: () => {
      toast.success(`${s?.shortId} silindi`);
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: IssueService,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
      void navigate({ to: "/o/$org/issues", params: { org: org.slug } });
    },
  });
  if (!s) return null;
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)} className="text-destructive">
        <Trash2Icon className="size-4" />
        Sil
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        tone="danger"
        icon={Trash2Icon}
        title={`${s.shortId} silinsin mi?`}
        description={`Hatanın ${s.timesSeen.toLocaleString("tr-TR")} olayı, ekleri, yorumları, geri bildirimleri ve sayaçları kalıcı olarak silinir. Aynı hata yeniden görülürse yeni bir kayıt açılır; yalnız bildirimleri durdurmak istiyorsanız silmek yerine yoksayın.`}
        confirmLabel="Kalıcı olarak sil"
        pendingLabel="Siliniyor…"
        onConfirm={() => del.mutateAsync({ issueIds: [s.id] }).catch(failWith)}
      />
    </>
  );
}

/** Deletes one event with its attachments, after a confirmation (administrators). */
export function DeleteEventButton({ eventId, onDeleted }: { eventId: string; onDeleted: () => void }) {
  const org = useOrg();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const del = useMutation(EventService.method.deleteEvent, {
    onSuccess: () => {
      toast.success("Olay silindi");
      for (const schema of [EventService, IssueService]) {
        void qc.invalidateQueries({
          queryKey: createConnectQueryKey({ schema, transport: org.transport, cardinality: undefined }),
        });
      }
      onDeleted();
    },
  });
  return (
    <>
      <Button variant="ghost" size="sm" className="px-2 text-destructive" onClick={() => setOpen(true)}>
        <Trash2Icon className="size-4" />
        <span className="sr-only sm:not-sr-only">Olayı sil</span>
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        tone="danger"
        icon={Trash2Icon}
        title="Olay silinsin mi?"
        description="Olay, ekleri ve onunla gönderilen geri bildirim kalıcı olarak silinir. Hata ve sayaçları kalır."
        confirmLabel="Kalıcı olarak sil"
        pendingLabel="Siliniyor…"
        onConfirm={() => del.mutateAsync({ id: eventId }).catch(failWith)}
      />
    </>
  );
}
