import { clone, create } from "@bufbuild/protobuf";
import { useMutation } from "@connectrpc/connect-query";
import { Field } from "@kentos/components/common/field";
import { Button } from "@kentos/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Input } from "@kentos/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Role } from "@/gen/izci/api/v1/common_pb";
import { type Project, ProjectService, ProjectSettingsSchema } from "@/gen/izci/api/v1/project_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { platformOptions } from "@/lib/platforms";
import { canAdmin } from "@/lib/session";
import { useInvalidateProjects, useUpdateProject } from "./use-project";

export function ProjectGeneral({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const update = useUpdateProject();
  const [name, setName] = useState(project.name);
  const [platform, setPlatform] = useState(project.platform || "other");
  const [rate, setRate] = useState(
    project.settings?.rateLimitPerMinute ? String(project.settings.rateLimitPerMinute) : "",
  );
  const [deleting, setDeleting] = useState(false);

  return (
    <div className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-10 py-6">
      <form
        className="grid gap-5"
        onSubmit={(e) => {
          e.preventDefault();
          const settings = clone(ProjectSettingsSchema, project.settings ?? create(ProjectSettingsSchema));
          settings.rateLimitPerMinute = Number(rate) || 0;
          update.mutate({
            slug: project.slug,
            name,
            platform: platform === "other" ? "" : platform,
            settings,
          });
        }}
      >
        <Field label="Proje adı" htmlFor="p-name">
          <Input
            id="p-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={!admin}
            required
          />
        </Field>
        <Field label="Kısa ad" hint="Adreslerde ve hata kimliklerinde (KALEM-1A) kullanılır; değiştirilemez.">
          <Input value={project.slug} disabled className="font-mono text-sm" />
        </Field>
        <Field label="Platform" htmlFor="p-platform">
          <Select value={platform} onValueChange={setPlatform} disabled={!admin}>
            <SelectTrigger id="p-platform" className="w-full">
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
        <Field
          label="Dakikalık olay sınırı"
          htmlFor="p-rate"
          hint="Boş ya da 0: yalnızca organizasyon kotası uygulanır."
        >
          <Input
            id="p-rate"
            type="number"
            min={0}
            value={rate}
            onChange={(e) => setRate(e.target.value)}
            disabled={!admin}
            className="w-40"
          />
        </Field>
        {admin ? (
          <div>
            <Button type="submit" disabled={update.isPending}>
              Kaydet
            </Button>
          </div>
        ) : null}
      </form>

      {org.role === Role.OWNER ? (
        <section className="rounded-lg border border-destructive/35 px-5 py-4">
          <h2 className="font-semibold text-md">Projeyi sil</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Projenin tüm hataları, olayları ve anahtarları kalıcı olarak silinir. Anahtarlarla gelen olaylar
            hemen reddedilir.
          </p>
          <Button variant="destructive" size="sm" className="mt-3" onClick={() => setDeleting(true)}>
            Projeyi sil
          </Button>
        </section>
      ) : null}
      <DeleteProjectDialog project={project} open={deleting} onOpenChange={setDeleting} />
    </div>
  );
}

function DeleteProjectDialog({
  project,
  open,
  onOpenChange,
}: {
  project: Project;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const org = useOrg();
  const navigate = useNavigate();
  const invalidate = useInvalidateProjects();
  const [confirm, setConfirm] = useState("");
  const del = useMutation(ProjectService.method.deleteProject, {
    onSuccess: () => {
      toast.success(`${project.name} silindi`);
      void invalidate();
      void navigate({ to: "/o/$org/projects", params: { org: org.slug } });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{project.name} silinsin mi?</DialogTitle>
          <DialogDescription>
            Bu işlem geri alınamaz. Onaylamak için projenin kısa adını yazın:{" "}
            <code className="font-mono text-foreground">{project.slug}</code>
          </DialogDescription>
        </DialogHeader>
        <Input
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          autoFocus
          className="font-mono text-sm"
        />
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Vazgeç
          </Button>
          <Button
            variant="destructive"
            disabled={confirm !== project.slug || del.isPending}
            onClick={() => del.mutate({ slug: project.slug, confirmSlug: confirm })}
          >
            Kalıcı olarak sil
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
