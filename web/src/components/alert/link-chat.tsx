import { useMutation, useQuery } from "@connectrpc/connect-query";
import { CheckCircle2Icon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { SecretValue } from "@/components/common/copy-button";
import { Field, FormError } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { AlertService, type CreateTelegramLinkCodeResponse } from "@/gen/izci/api/v1/alert_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorMessage } from "@/lib/errors";

/** Creates a link code and waits until the chat shows up. */
export function LinkChatDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const projects = useQuery(ProjectService.method.listProjects, {}, { enabled: open });
  const [project, setProject] = useState("*");
  const [code, setCode] = useState<CreateTelegramLinkCodeResponse>();
  const create = useMutation(AlertService.method.createTelegramLinkCode, { onSuccess: setCode });
  // While the code is shown, watch for the new chat.
  const channels = useQuery(
    AlertService.method.listChannels,
    {},
    { enabled: open, refetchInterval: code ? 3000 : false },
  );
  const initial = useRef<number | undefined>(undefined);
  const count = channels.data?.channels.length;
  useEffect(() => {
    if (open && initial.current === undefined && count !== undefined) initial.current = count;
  }, [open, count]);
  const linked =
    code && initial.current !== undefined && count !== undefined && count > initial.current
      ? channels.data?.channels[0]
      : undefined;

  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v) {
      setCode(undefined);
      create.reset();
      initial.current = undefined;
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Telegram sohbeti bağla</DialogTitle>
          <DialogDescription>
            Bildirimler bir gruba, forum grubunun bir konusuna ya da botla özel sohbete gidebilir.
          </DialogDescription>
        </DialogHeader>
        {linked ? (
          <div className="flex items-start gap-3 rounded-lg border border-primary/40 px-4 py-3">
            <CheckCircle2Icon className="mt-0.5 size-5 text-primary" />
            <div>
              <p className="font-medium text-[14px]">{linked.name} bağlandı</p>
              <p className="text-[13px] text-muted-foreground">
                İlk sohbetinizse, üretimdeki yeni ve geri dönen hatalar için iki kural da eklendi.
              </p>
            </div>
          </div>
        ) : code ? (
          <ol className="grid gap-4 text-[13.5px]">
            <li className="grid gap-2">
              <span>
                <span className="mr-2 font-medium text-muted-foreground tabular">1</span>@{code.botUsername}{" "}
                botunu gruba ekleyin.
              </span>
              <div>
                <Button asChild variant="outline" size="sm">
                  <a href={code.groupLink} target="_blank" rel="noreferrer">
                    Grup seçerek ekle
                  </a>
                </Button>
              </div>
            </li>
            <li className="grid gap-2">
              <span>
                <span className="mr-2 font-medium text-muted-foreground tabular">2</span>Grupta ya da bildirim
                almak istediğiniz konuda şu komutu gönderin:
              </span>
              <SecretValue value={`/baglan ${code.code}`} />
            </li>
            <li className="text-muted-foreground">
              Bildirimleri kendinize almak için{" "}
              <a
                href={code.privateLink}
                target="_blank"
                rel="noreferrer"
                className="text-foreground underline underline-offset-4"
              >
                botla özel sohbet
              </a>{" "}
              açmanız yeterli. Kod <RelativeTime value={code.expiresAt} /> geçersiz olur.
            </li>
          </ol>
        ) : (
          <div className="grid gap-4">
            <Field label="Bildirilecek projeler" htmlFor="link-project">
              <Select value={project} onValueChange={setProject}>
                <SelectTrigger id="link-project" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="*">Tüm projeler</SelectItem>
                  {projects.data?.projects.map((p) => (
                    <SelectItem key={p.id} value={p.slug}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <FormError message={create.error ? errorMessage(create.error) : undefined} />
          </div>
        )}
        <DialogFooter>
          {linked || code ? (
            <Button onClick={() => close(false)}>{linked ? "Tamam" : "Kapat"}</Button>
          ) : (
            <>
              <Button variant="ghost" onClick={() => close(false)}>
                Vazgeç
              </Button>
              <Button
                disabled={create.isPending}
                onClick={() => create.mutate({ project: project === "*" ? "" : project })}
              >
                Bağlantı kodu oluştur
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
