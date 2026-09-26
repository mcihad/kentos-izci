import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Field, FormError } from "@/components/common/field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AuthService } from "@/gen/izci/api/v1/auth_pb";
import { UserService } from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { errorField, errorMessage } from "@/lib/errors";
import { sessionQuery, useSession } from "@/lib/session";

export function Profile() {
  const user = useSession()?.user;
  const qc = useQueryClient();
  const [name, setName] = useState(user?.name ?? "");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const profile = useMutation(UserService.method.updateProfile, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Profil kaydedildi");
      void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const password = useMutation(AuthService.method.changePassword, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Parolanız değiştirildi. Diğer cihazlardaki oturumlar kapatıldı.");
      setCurrent("");
      setNext("");
    },
  });
  if (!user) return null;
  return (
    <div className="grid max-w-2xl grid-cols-1 gap-10 py-6">
      <form
        className="grid gap-5"
        onSubmit={(e) => {
          e.preventDefault();
          profile.mutate({ name });
        }}
      >
        <h2 className="font-semibold text-[15px]">Profil</h2>
        <Field label="Ad soyad" htmlFor="me-name">
          <Input id="me-name" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field
          label="E-posta"
          hint={
            user.linkedProviders.length
              ? `Bağlı giriş yöntemleri: ${user.linkedProviders.join(", ")}`
              : undefined
          }
        >
          <Input value={user.email} disabled />
        </Field>
        <div>
          <Button type="submit" disabled={profile.isPending}>
            Kaydet
          </Button>
        </div>
      </form>
      {user.hasPassword ? (
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            password.mutate({ currentPassword: current, newPassword: next });
          }}
        >
          <h2 className="font-semibold text-[15px]">Parola</h2>
          <Field label="Şu anki parola" htmlFor="pw-current">
            <Input
              id="pw-current"
              type="password"
              autoComplete="current-password"
              required
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </Field>
          <Field
            label="Yeni parola"
            htmlFor="pw-new"
            hint="En az 10 karakter."
            error={
              password.error && errorField(password.error) === "new_password"
                ? errorMessage(password.error)
                : undefined
            }
          >
            <Input
              id="pw-new"
              type="password"
              autoComplete="new-password"
              minLength={10}
              required
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
          </Field>
          <FormError
            message={
              password.error && errorField(password.error) !== "new_password"
                ? errorMessage(password.error)
                : undefined
            }
          />
          <div>
            <Button type="submit" disabled={password.isPending}>
              Parolayı değiştir
            </Button>
          </div>
        </form>
      ) : (
        <p className="text-[13px] text-muted-foreground">
          Hesabınız kimlik sağlayıcısıyla giriş yapıyor; parola tanımlı değil.
        </p>
      )}
    </div>
  );
}
