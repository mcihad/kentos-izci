import { createConnectQueryKey } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { useSensitiveMutation } from "@/lib/reauth";

/** Refreshes every cached ProjectService response of the organization. */
export function useInvalidateProjects() {
  const org = useOrg();
  const qc = useQueryClient();
  return () =>
    qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: ProjectService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
}

/**
 * Saves project changes with feedback. Shortening the retention asks the
 * user to confirm their identity first.
 */
export function useUpdateProject(done = "Değişiklikler kaydedildi") {
  const invalidate = useInvalidateProjects();
  return useSensitiveMutation(ProjectService.method.updateProject, {
    onSuccess: () => {
      toast.success(done);
      void invalidate();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
}

/** Splits a textarea into trimmed, non-empty lines. */
export function lines(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);
}
