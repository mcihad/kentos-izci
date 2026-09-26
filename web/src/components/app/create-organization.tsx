import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { Field, FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { OrganizationService } from "@/gen/izci/api/v1/organization_pb";
import { baseTransport } from "@/lib/api";
import { errorField, errorMessage } from "@/lib/errors";
import { sessionQuery } from "@/lib/session";
import { slugify } from "@/lib/slug";

/** Creates an organization and switches to it. */
export function CreateOrganizationForm({ onDone }: { onDone?: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [edited, setEdited] = useState(false);
  const create = useMutation(OrganizationService.method.createOrganization, {
    transport: baseTransport,
    onSuccess: async (res) => {
      await qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
      onDone?.();
      const org = res.organization?.slug;
      if (org) void navigate({ to: "/o/$org", params: { org } });
    },
  });
  const fieldError = (f: string) =>
    create.error && errorField(create.error) === f ? errorMessage(create.error) : undefined;

  return (
    <form
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate({ name, slug });
      }}
    >
      <Field label="Organizasyon adı" htmlFor="new-org-name" error={fieldError("name")}>
        <Input
          id="new-org-name"
          required
          autoFocus
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            if (!edited) setSlug(slugify(e.target.value));
          }}
        />
      </Field>
      <Field
        label="Kısa ad"
        htmlFor="new-org-slug"
        error={fieldError("slug")}
        hint="Adreslerde görünür: /o/kisa-ad"
      >
        <Input
          id="new-org-slug"
          required
          className="font-mono text-[13px]"
          value={slug}
          onChange={(e) => {
            setSlug(e.target.value.toLowerCase());
            setEdited(true);
          }}
        />
      </Field>
      <FormError
        message={create.error && !errorField(create.error) ? errorMessage(create.error) : undefined}
      />
      <Button type="submit" disabled={create.isPending}>
        {create.isPending ? "Oluşturuluyor…" : "Organizasyonu oluştur"}
      </Button>
    </form>
  );
}
