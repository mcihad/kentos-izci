import { ErrorState as KitErrorState } from "@kentos/components/common/states";
import { errorMessage } from "@/lib/errors";

export { EmptyState } from "@kentos/components/common/states";

/** Shown when loading failed: the API error in Turkish and a retry. */
export function ErrorState({
  error,
  onRetry,
  className,
}: {
  error: unknown;
  onRetry?: () => void;
  className?: string;
}) {
  return <KitErrorState message={errorMessage(error)} onRetry={onRetry} className={className} />;
}
