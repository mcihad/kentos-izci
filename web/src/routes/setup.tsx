import { createQueryOptions, useMutation } from "@connectrpc/connect-query";
import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { CheckIcon } from "lucide-react";
import { type FormEvent, useState } from "react";
import { AuthLayout } from "@/components/app/auth-layout";
import { SecretValue } from "@/components/common/copy-button";
import { Field, FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { AuthService, type CompleteSetupResponse } from "@/gen/izci/api/v1/auth_pb";
import { baseTransport } from "@/lib/api";
import { errorField, errorMessage } from "@/lib/errors";
import { platformOptions } from "@/lib/platforms";
import { storeSession } from "@/lib/session";
import { slugify } from "@/lib/slug";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/setup")({
  beforeLoad: async ({ context }) => {
    const state = await context.queryClient.fetchQuery(
      createQueryOptions(AuthService.method.getSetupState, {}, { transport: baseTransport }),
    );
    if (!state.setupRequired) throw redirect({ to: "/login" });
  },
  component: SetupPage,
});

const steps = ["Yönetici hesabı", "Organizasyon", "İlk proje"];

// Which step shows the field an INVALID_ARGUMENT error points at.
const fieldStep: Record<string, number> = {
  name: 0,
  email: 0,
  password: 0,
  organization_name: 1,
  organization_slug: 1,
  project_name: 2,
  project_platform: 2,
};

function SetupPage() {
  const { queryClient } = Route.useRouteContext();
  const [step, setStep] = useState(0);
  const [form, setForm] = useState({
    name: "",
    email: "",
    password: "",
    organizationName: "",
    organizationSlug: "",
    slugEdited: false,
    projectName: "",
    projectPlatform: "dotnet",
  });
  const [done, setDone] = useState<CompleteSetupResponse>();
  const setup = useMutation(AuthService.method.completeSetup, {
    onSuccess: (res) => {
      storeSession(queryClient, res.session);
      setDone(res);
    },
    onError: (err) => {
      const s = fieldStep[errorField(err)];
      if (s !== undefined) setStep(s);
    },
  });
  const set = (patch: Partial<typeof form>) => setForm((f) => ({ ...f, ...patch }));
  const fieldError = (f: string) =>
    setup.error && errorField(setup.error) === f ? errorMessage(setup.error) : undefined;

  if (done) {
    const org = form.organizationSlug;
    return (
      <AuthLayout wide>
        <div className="space-y-6">
          <div className="space-y-1.5">
            <h1 className="font-semibold text-2xl tracking-tight">İzci hazır</h1>
            <p className="text-muted-foreground text-sm leading-relaxed">
              <strong className="font-medium text-foreground">{form.projectName}</strong> projesi için bir API
              anahtarı oluşturuldu. Uygulamanız hataları bu anahtarla gönderir. Anahtar yalnızca şimdi
              gösterilir; güvenli bir yere kaydedin.
            </p>
          </div>
          <SecretValue value={done.apiKey} />
          <div className="flex flex-wrap gap-2">
            <Button asChild>
              <Link
                to="/o/$org/projects/$slug"
                params={{ org, slug: done.projectSlug }}
                search={{ tab: "setup" }}
              >
                Kurulum yönergelerini aç
              </Link>
            </Button>
            <Button asChild variant="outline">
              <Link to="/o/$org" params={{ org }}>
                Genel bakışa geç
              </Link>
            </Button>
          </div>
        </div>
      </AuthLayout>
    );
  }

  const next = (e: FormEvent) => {
    e.preventDefault();
    if (step < steps.length - 1) {
      setStep(step + 1);
      return;
    }
    setup.mutate({
      name: form.name,
      email: form.email,
      password: form.password,
      organizationName: form.organizationName,
      organizationSlug: form.organizationSlug,
      projectName: form.projectName,
      projectPlatform: form.projectPlatform === "other" ? "" : form.projectPlatform,
    });
  };

  return (
    <AuthLayout wide>
      <div className="space-y-7">
        <div className="space-y-1.5">
          <h1 className="font-semibold text-2xl tracking-tight">İzci'yi kurun</h1>
          <p className="text-md text-muted-foreground">
            Üç adımda yönetici hesabını, organizasyonunuzu ve ilk projenizi oluşturun.
          </p>
        </div>

        <ol className="flex items-center gap-2 text-sm">
          {steps.map((label, i) => (
            <li key={label} className="flex flex-1 items-center gap-2">
              <button
                type="button"
                disabled={i > step}
                onClick={() => setStep(i)}
                className={cn(
                  "flex items-center gap-2 disabled:cursor-default",
                  i === step ? "text-foreground" : "text-muted-foreground",
                )}
              >
                <span
                  className={cn(
                    "grid size-6 shrink-0 place-items-center rounded-full border font-medium text-xs tabular",
                    i < step && "border-primary bg-primary text-primary-foreground",
                    i === step && "border-primary text-brand-ink",
                  )}
                >
                  {i < step ? <CheckIcon className="size-3.5" /> : i + 1}
                </span>
                <span className="hidden sm:inline">{label}</span>
              </button>
              {i < steps.length - 1 ? <span className="h-px flex-1 bg-border" /> : null}
            </li>
          ))}
        </ol>

        <form className="grid gap-4" onSubmit={next}>
          {step === 0 ? (
            <>
              <Field label="Ad soyad" htmlFor="name" error={fieldError("name")}>
                <Input
                  id="name"
                  required
                  autoFocus
                  value={form.name}
                  onChange={(e) => set({ name: e.target.value })}
                />
              </Field>
              <Field label="E-posta" htmlFor="email" error={fieldError("email")}>
                <Input
                  id="email"
                  type="email"
                  autoComplete="username"
                  required
                  value={form.email}
                  onChange={(e) => set({ email: e.target.value })}
                />
              </Field>
              <Field
                label="Parola"
                htmlFor="password"
                error={fieldError("password")}
                hint="En az 10 karakter. Uzun bir cümle hem güvenli hem akılda kalıcıdır."
              >
                <Input
                  id="password"
                  type="password"
                  autoComplete="new-password"
                  required
                  minLength={10}
                  value={form.password}
                  onChange={(e) => set({ password: e.target.value })}
                />
              </Field>
            </>
          ) : null}
          {step === 1 ? (
            <>
              <Field label="Organizasyon adı" htmlFor="org" error={fieldError("organization_name")}>
                <Input
                  id="org"
                  required
                  autoFocus
                  placeholder="KentOS"
                  value={form.organizationName}
                  onChange={(e) =>
                    set({
                      organizationName: e.target.value,
                      organizationSlug: form.slugEdited ? form.organizationSlug : slugify(e.target.value),
                    })
                  }
                />
              </Field>
              <Field
                label="Kısa ad"
                htmlFor="slug"
                error={fieldError("organization_slug")}
                hint="Adreslerde görünür: /o/kisa-ad. Küçük harf, rakam ve tire kullanın."
              >
                <Input
                  id="slug"
                  required
                  pattern="[a-z][a-z0-9\-]{0,48}[a-z0-9]"
                  className="font-mono text-sm"
                  value={form.organizationSlug}
                  onChange={(e) => set({ organizationSlug: e.target.value.toLowerCase(), slugEdited: true })}
                />
              </Field>
            </>
          ) : null}
          {step === 2 ? (
            <>
              <Field
                label="Proje adı"
                htmlFor="project"
                error={fieldError("project_name")}
                hint="Hata gönderecek ilk uygulamanız, örneğin KentOS Kalem."
              >
                <Input
                  id="project"
                  required
                  autoFocus
                  value={form.projectName}
                  onChange={(e) => set({ projectName: e.target.value })}
                />
              </Field>
              <Field label="Platform" htmlFor="platform">
                <Select value={form.projectPlatform} onValueChange={(v) => set({ projectPlatform: v })}>
                  <SelectTrigger id="platform" className="w-full">
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
            </>
          ) : null}

          <FormError
            message={setup.error && !errorField(setup.error) ? errorMessage(setup.error) : undefined}
          />
          <div className="flex items-center justify-between gap-3 pt-1">
            {step > 0 ? (
              <Button type="button" variant="ghost" onClick={() => setStep(step - 1)}>
                Geri
              </Button>
            ) : (
              <span />
            )}
            <Button type="submit" disabled={setup.isPending}>
              {step < steps.length - 1 ? "Devam et" : setup.isPending ? "Kuruluyor…" : "Kurulumu tamamla"}
            </Button>
          </div>
        </form>
      </div>
    </AuthLayout>
  );
}
