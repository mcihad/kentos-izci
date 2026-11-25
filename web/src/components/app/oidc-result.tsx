import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import { toast } from "sonner";
import { oidcErrors } from "@/lib/errors";
import { sessionQuery } from "@/lib/session";

/** Query parameters the OIDC callback adds to the page it returns to. */
export const oidcResultParams = ["oidc_linked", "oidc_reauth", "oidc_error"];

/**
 * Reports what linking an identity provider or re-authenticating with one
 * ended in, then drops it from the address.
 */
export default function OidcResult({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const qc = useQueryClient();
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const linked = params.get("oidc_linked");
    const reauth = params.get("oidc_reauth");
    const error = params.get("oidc_error");
    if (!linked && !reauth && !error) {
      onDone();
      return;
    }
    // A fixed id keeps a repeated run from showing the message twice.
    if (error) {
      toast.error(oidcErrors[error] ?? "Kimlik sağlayıcısıyla işlem tamamlanamadı. Yeniden deneyin.", {
        id: "oidc-result",
      });
    } else {
      toast.success(
        linked
          ? "Kimlik sağlayıcısı hesabınıza bağlandı"
          : "Kimliğiniz doğrulandı. İşleme devam edebilirsiniz.",
        { id: "oidc-result" },
      );
      void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
    }
    for (const p of oidcResultParams) params.delete(p);
    const { pathname, hash } = router.state.location;
    const rest = params.toString();
    router.history.replace(`${pathname}${rest ? `?${rest}` : ""}${hash ? `#${hash}` : ""}`);
    onDone();
  }, [router, qc, onDone]);
  return null;
}
