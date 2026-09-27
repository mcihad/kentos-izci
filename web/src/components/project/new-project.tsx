import { useMutation } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { SecretValue } from "@/components/common/copy-button";
import { Field, FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { type CreateProjectResponse, ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorField, errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { platformOptions } from "@/lib/platforms";
import { slugify } from "@/lib/slug";
import { useInvalidateProjects } from "./use-project";

export function NewProjectDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const org = useOrg();
  const invalidate = useInvalidateProjects();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [edited, setEdited] = useState(false);
  const [platform, setPlatform] = useState("dotnet");
  const [created, setCreated] = useState<CreateProjectResponse>();
  const create = useMutation(ProjectService.method.createProject, {
    onSuccess: (res) => {
      setCreated(res);
      void invalidate();
    },
  });
  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v) {
      setCreated(undefined);
      setName("");
      setSlug("");
      setEdited(false);
      create.reset();
    }
  };
  const fieldError = (f: string) =>
    create.error && errorField(create.error) === f ? errorMessage(create.error) : undefined;

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-lg">
        {created?.project ? (
          <>
            <DialogHeader>
              <DialogTitle>{created.project.name} oluşturuldu</DialogTitle>
              <DialogDescription>
                İlk API anahtarı hazır. Anahtar yalnızca şimdi gösterilir; kopyalayıp güvenli bir yere
                kaydedin.
              </DialogDescription>
            </DialogHeader>
            <SecretValue value={created.apiKeySecret} />
            <DialogFooter>
              <Button asChild onClick={() => close(false)}>
                <Link
                  to="/o/$org/projects/$slug"
                  params={{ org: org.slug, slug: created.project.slug }}
                  search={{ tab: "setup" }}
                >
                  Kurulum yönergelerine geç
                </Link>
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Yeni proje</DialogTitle>
              <DialogDescription>
                Her uygulama ya da servis için bir proje açın; hatalar projeye göre gruplanır.
              </DialogDescription>
            </DialogHeader>
            <form
              className="grid gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                create.mutate({ name, slug, platform: platform === "other" ? "" : platform });
              }}
            >
              <Field label="Ad" htmlFor="np-name" error={fieldError("name")}>
                <Input
                  id="np-name"
                  required
                  autoFocus
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value);
                    if (!edited) setSlug(slugify(e.target.value));
                  }}
                  placeholder="KentOS Kalem"
                />
              </Field>
              <Field
                label="Kısa ad"
                htmlFor="np-slug"
                error={fieldError("slug")}
                hint="Hata kimliklerinde görünür: KALEM-1A"
              >
                <Input
                  id="np-slug"
                  value={slug}
                  onChange={(e) => {
                    setSlug(e.target.value.toLowerCase());
                    setEdited(true);
                  }}
                  className="font-mono text-sm"
                />
              </Field>
              <Field label="Platform" htmlFor="np-platform">
                <Select value={platform} onValueChange={setPlatform}>
                  <SelectTrigger id="np-platform" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {platformOptions.map((p) => (
                      <SelectItem key={p.value} value={p.value}>
                        {p.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <FormError
                message={create.error && !errorField(create.error) ? errorMessage(create.error) : undefined}
              />
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => close(false)}>
                  Vazgeç
                </Button>
                <Button type="submit" disabled={create.isPending}>
                  Projeyi oluştur
                </Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
