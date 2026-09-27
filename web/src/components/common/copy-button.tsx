import { CheckIcon, CopyIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

export async function copyText(text: string, done = "Kopyalandı") {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(done);
    return true;
  } catch {
    toast.error("Panoya kopyalanamadı. Metni seçip elle kopyalayın.");
    return false;
  }
}

/** Icon button that copies a value and confirms with a check mark. */
export function CopyButton({
  value,
  label = "Kopyala",
  className,
}: {
  value: string;
  label?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className={cn("size-7 text-muted-foreground", className)}
          aria-label={label}
          onClick={async () => {
            if (await copyText(value)) {
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            }
          }}
        >
          {copied ? <CheckIcon className="size-3.5 text-primary" /> : <CopyIcon className="size-3.5" />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** A secret or snippet shown once, with a copy button. */
export function SecretValue({ value, className }: { value: string; className?: string }) {
  return (
    <div className={cn("flex items-center gap-1 rounded-md border bg-code py-1 pr-1 pl-3", className)}>
      <code className="min-w-0 flex-1 truncate text-sm">{value}</code>
      <CopyButton value={value} />
    </div>
  );
}
