import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { initials } from "@/lib/format";
import { cn } from "@/lib/utils";

/** Avatar with initials when there is no picture. */
export function UserAvatar({ name, src, className }: { name: string; src?: string; className?: string }) {
  return (
    <Avatar className={cn("size-6", className)}>
      {src ? <AvatarImage src={src} alt="" /> : null}
      <AvatarFallback className="bg-secondary font-medium text-2xs text-secondary-foreground">
        {initials(name)}
      </AvatarFallback>
    </Avatar>
  );
}
