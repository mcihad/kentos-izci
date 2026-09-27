import { useMutation } from "@connectrpc/connect-query";
import { useState } from "react";
import { FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";
import { type AiRun, AiRunMode, AiService } from "@/gen/izci/api/v1/ai_pb";
import { errorMessage } from "@/lib/errors";

/** Starts an analysis of an issue, optionally with a code fix. */
export function StartRunDialog({
  issueId,
  eventId,
  hasRepository,
  open,
  onOpenChange,
  onStarted,
}: {
  issueId: string;
  eventId?: string;
  hasRepository: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onStarted: (run: AiRun) => void;
}) {
  const [mode, setMode] = useState<AiRunMode>(hasRepository ? AiRunMode.FIX : AiRunMode.ANALYZE);
  const [instructions, setInstructions] = useState("");
  const start = useMutation(AiService.method.startRun, {
    onSuccess: (res) => {
      if (res.run) onStarted(res.run);
      onOpenChange(false);
      setInstructions("");
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            start.mutate({ issueId, eventId: eventId ?? "", mode, instructions: instructions.trim() });
          }}
        >
          <DialogHeader>
            <DialogTitle>Yapay zekâ ile analiz</DialogTitle>
            <DialogDescription>
              İzci hatanın olaylarını, yığın izini ve {hasRepository ? "bağlı depodaki kodu" : "bağlamını"}{" "}
              opencode'a verir. Sonuç birkaç dakika içinde burada görünür.
            </DialogDescription>
          </DialogHeader>
          <RadioGroup
            value={String(mode)}
            onValueChange={(v) => setMode(Number(v) as AiRunMode)}
            className="gap-2"
          >
            <Label
              htmlFor="mode-fix"
              className="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-60 has-[[data-state=checked]]:border-primary/60"
            >
              <RadioGroupItem
                id="mode-fix"
                value={String(AiRunMode.FIX)}
                disabled={!hasRepository}
                className="mt-0.5"
              />
              <span className="grid gap-0.5">
                <span className="font-medium text-sm">Kök nedeni bul ve düzeltme öner</span>
                <span className="font-normal text-xs text-muted-foreground leading-relaxed">
                  {hasRepository
                    ? "Ajan kodda en küçük güvenli değişikliği yapar; siz inceleyip dal ya da çekme isteği açarsınız."
                    : "Bu seçenek için projeye bir depo bağlanmalı."}
                </span>
              </span>
            </Label>
            <Label
              htmlFor="mode-analyze"
              className="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 has-[[data-state=checked]]:border-primary/60"
            >
              <RadioGroupItem id="mode-analyze" value={String(AiRunMode.ANALYZE)} className="mt-0.5" />
              <span className="grid gap-0.5">
                <span className="font-medium text-sm">Yalnız analiz et</span>
                <span className="font-normal text-xs text-muted-foreground leading-relaxed">
                  Kod değişmez; kök neden ve önerilen düzeltme rapor olarak yazılır.
                </span>
              </span>
            </Label>
          </RadioGroup>
          <div className="grid gap-1.5">
            <Label htmlFor="ai-instructions" className="text-sm">
              Ek bilgi <span className="font-normal text-muted-foreground">(isteğe bağlı)</span>
            </Label>
            <Textarea
              id="ai-instructions"
              value={instructions}
              onChange={(e) => setInstructions(e.target.value)}
              maxLength={4000}
              placeholder="Örneğin: hata yalnız eski tarifedeki abonelerde görülüyor."
              className="min-h-20 text-sm"
            />
          </div>
          <FormError message={start.error ? errorMessage(start.error) : undefined} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={start.isPending}>
              Analizi başlat
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
