import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { initials } from "@/lib/format";
import { cn } from "@/lib/utils";

/** Avatar with initials when there is no picture. */
export function UserAvatar({
  name,
  src,
  className,
  fallbackClassName,
}: {
  name: string;
  src?: string;
  className?: string;
  fallbackClassName?: string;
}) {
  return (
    <Avatar className={cn("size-6", className)}>
      {src ? <AvatarImage src={src} alt="" /> : null}
      <AvatarFallback
        className={cn(
          "bg-(--status-info-bg) font-semibold text-(--status-info-fg) text-2xs",
          fallbackClassName,
        )}
      >
        {initials(name)}
      </AvatarFallback>
    </Avatar>
  );
}
