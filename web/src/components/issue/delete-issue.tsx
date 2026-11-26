import { createConnectQueryKey, useMutation } from "@connectrpc/connect-query";
import { Button } from "@kentos/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { EventService } from "@/gen/izci/api/v1/event_pb";
import { type Issue, IssueService } from "@/gen/izci/api/v1/issue_pb";
import { errorMessage } from "@/lib/errors";
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
      setOpen(false);
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: IssueService,
          transport: org.transport,
          cardinality: undefined,
        }),
      });
      void navigate({ to: "/o/$org/issues", params: { org: org.slug } });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  if (!s) return null;
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)} className="text-destructive">
        <Trash2Icon className="size-4" />
        Sil
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{s.shortId} silinsin mi?</DialogTitle>
            <DialogDescription>
              Hatanın {s.timesSeen.toLocaleString("tr-TR")} olayı, ekleri, yorumları, geri bildirimleri ve
              sayaçları kalıcı olarak silinir. Aynı hata yeniden görülürse yeni bir kayıt açılır; yalnız
              bildirimleri durdurmak istiyorsanız silmek yerine yoksayın.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              Vazgeç
            </Button>
            <Button
              variant="destructive"
              disabled={del.isPending}
              onClick={() => del.mutate({ issueIds: [s.id] })}
            >
              Kalıcı olarak sil
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
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
      setOpen(false);
      for (const schema of [EventService, IssueService]) {
        void qc.invalidateQueries({
          queryKey: createConnectQueryKey({ schema, transport: org.transport, cardinality: undefined }),
        });
      }
      onDeleted();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <>
      <Button variant="ghost" size="sm" className="px-2 text-destructive" onClick={() => setOpen(true)}>
        <Trash2Icon className="size-4" />
        <span className="sr-only sm:not-sr-only">Olayı sil</span>
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Olay silinsin mi?</DialogTitle>
            <DialogDescription>
              Olay, ekleri ve onunla gönderilen geri bildirim kalıcı olarak silinir. Hata ve sayaçları kalır.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              Vazgeç
            </Button>
            <Button
              variant="destructive"
              disabled={del.isPending}
              onClick={() => del.mutate({ id: eventId })}
            >
              Kalıcı olarak sil
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
