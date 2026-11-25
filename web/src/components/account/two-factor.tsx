import { createConnectQueryKey, useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { CopyIcon, DownloadIcon, ShieldCheckIcon, TriangleAlertIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { CopyButton, copyText } from "@/components/common/copy-button";
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
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { UserService } from "@/gen/izci/api/v1/user_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage, errorReason } from "@/lib/errors";
import { useSensitiveMutation } from "@/lib/reauth";
import { sessionQuery, useSession } from "@/lib/session";

/**
 * A one-time code field: six digits from an authenticator app or, when
 * recovery is set, a recovery code. Password managers fill it through
 * autocomplete="one-time-code".
 */
export function CodeInput({
  id,
  value,
  onChange,
  recovery = false,
  autoFocus = false,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  recovery?: boolean;
  autoFocus?: boolean;
}) {
  return (
    <Input
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      inputMode={recovery ? "text" : "numeric"}
      autoComplete="one-time-code"
      autoCapitalize="none"
      autoCorrect="off"
      spellCheck={false}
      maxLength={recovery ? 32 : 7}
      placeholder={recovery ? "xxxxx-xxxxx" : undefined}
      required
      autoFocus={autoFocus}
      className="h-10 font-mono text-lg tabular tracking-[0.2em]"
    />
  );
}

/** Digits of a typed TOTP code without spaces. */
export function cleanCode(v: string): string {
  return v.replace(/[\s-]/g, "");
}

/**
 * The QR code of an otpauth URI. The encoder loads on demand; the code is
 * black on white in both themes so that every camera reads it.
 */
function QrCode({ value }: { value: string }) {
  const [qr, setQr] = useState<{ size: number; path: string }>();
  useEffect(() => {
    let live = true;
    void import("uqr").then(({ encode }) => {
      const code = encode(value, { ecc: "M", border: 2 });
      let path = "";
      for (const [y, row] of code.data.entries()) {
        for (const [x, dark] of row.entries()) {
          if (dark) path += `M${x} ${y}h1v1h-1z`;
        }
      }
      if (live) setQr({ size: code.size, path });
    });
    return () => {
      live = false;
    };
  }, [value]);
  if (!qr) return <Skeleton className="size-44 shrink-0 rounded-md" />;
  return (
    <svg
      viewBox={`0 0 ${qr.size} ${qr.size}`}
      role="img"
      aria-label="Doğrulama uygulaması için QR kodu"
      shapeRendering="crispEdges"
      className="size-44 shrink-0 rounded-md border bg-white"
    >
      <title>Doğrulama uygulaması için QR kodu</title>
      <path d={qr.path} fill="#000" />
    </svg>
  );
}

/**
 * Turning two-factor authentication on: add the secret to an authenticator
 * app, confirm with its code. onEnabled receives the recovery codes.
 */
export function TwoFactorSetup({ onEnabled }: { onEnabled: (codes: string[]) => void }) {
  const [code, setCode] = useState("");
  const begin = useSensitiveMutation(UserService.method.beginTwoFactorSetup, {
    transport: baseTransport,
    onError: (err) => toast.error(errorMessage(err)),
  });
  const enable = useMutation(UserService.method.enableTwoFactor, {
    transport: baseTransport,
    onSuccess: (res) => onEnabled(res.recoveryCodes),
    onError: (err) => {
      if (errorReason(err) === ErrorReason.TWO_FACTOR_EXPIRED) {
        toast.error("Kurulumun süresi doldu. Yeniden başlatın.");
        begin.reset();
        setCode("");
      }
    },
  });
  if (!begin.data) {
    return (
      <div>
        <Button onClick={() => begin.mutate({})} disabled={begin.isPending}>
          İki adımlı doğrulamayı aç
        </Button>
      </div>
    );
  }
  const { secret, uri } = begin.data;
  const wrong = enable.error && errorReason(enable.error) !== ErrorReason.TWO_FACTOR_EXPIRED;
  return (
    <ol className="grid gap-6">
      <li className="grid gap-3">
        <div>
          <p className="font-medium text-md">1. Doğrulama uygulamanızla QR kodu okutun</p>
          <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
            Google Authenticator, Microsoft Authenticator, 1Password, Bitwarden gibi zamana dayalı kod üreten
            herhangi bir uygulama olur.
          </p>
        </div>
        <div className="flex flex-wrap items-start gap-5">
          <QrCode value={uri} />
          <div className="grid min-w-0 flex-1 basis-52 gap-1.5">
            <p className="text-sm text-muted-foreground">
              QR kodu okutamıyorsanız bu anahtarı uygulamaya elle girin:
            </p>
            <div className="flex items-center gap-1 rounded-md border bg-code py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 break-all font-mono text-sm">
                {secret.match(/.{1,4}/g)?.join(" ")}
              </code>
              <CopyButton value={secret} label="Anahtarı kopyala" />
            </div>
          </div>
        </div>
      </li>
      <li>
        <form
          className="grid gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            enable.mutate({ code: cleanCode(code) });
          }}
        >
          <Field
            label="2. Uygulamanın gösterdiği 6 haneli kodu girin"
            htmlFor="tf-enable-code"
            error={wrong ? errorMessage(enable.error) : undefined}
          >
            <div className="max-w-48">
              <CodeInput id="tf-enable-code" value={code} onChange={setCode} />
            </div>
          </Field>
          <div>
            <Button type="submit" disabled={enable.isPending || cleanCode(code).length < 6}>
              {enable.isPending ? "Doğrulanıyor…" : "Doğrula ve aç"}
            </Button>
          </div>
        </form>
      </li>
    </ol>
  );
}

/** Recovery codes, shown once right after they are created. */
export function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const email = useSession()?.user?.email ?? "";
  const text = codes.join("\n");
  const download = () => {
    const body = `İzci kurtarma kodları${email ? ` (${email})` : ""}\n\n${text}\n\nHer kod bir kez kullanılabilir.\n`;
    const url = URL.createObjectURL(new Blob([body], { type: "text/plain;charset=utf-8" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "izci-kurtarma-kodlari.txt";
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <div className="grid gap-4">
      <div>
        <h3 className="font-semibold text-md">Kurtarma kodlarınızı saklayın</h3>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          Telefonunuza erişemezseniz bu kodlardan biriyle giriş yaparsınız. Her kod bir kez kullanılır ve bu
          kodlar bir daha gösterilmez; parola yöneticinizde ya da güvenli bir yerde saklayın.
        </p>
      </div>
      <ul className="grid grid-cols-2 gap-x-8 gap-y-1.5 rounded-lg border bg-code px-4 py-3 font-mono text-sm tabular sm:w-fit">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={() => void copyText(text, "Kurtarma kodları kopyalandı")}>
          <CopyIcon />
          Kopyala
        </Button>
        <Button variant="outline" onClick={download}>
          <DownloadIcon />
          İndir
        </Button>
        <Button onClick={onDone}>Kodları sakladım</Button>
      </div>
    </div>
  );
}

type Confirm = "disable" | "regenerate";

/** Account tab: turn two-factor authentication on or off, renew recovery codes. */
export function TwoFactor() {
  const session = useSession();
  const qc = useQueryClient();
  const [codes, setCodes] = useState<string[]>();
  const [confirm, setConfirm] = useState<Confirm>();
  // Turning two-factor authentication on or off ends the other sessions.
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
    void qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: UserService.method.listSessions,
        transport: baseTransport,
        cardinality: "finite",
      }),
    });
  };
  const user = session?.user;
  if (!session || !user) return null;
  const requiring = session.memberships
    .filter((m) => m.requiresTwoFactor)
    .map((m) => m.organization?.name ?? "");
  return (
    <div className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-6 py-6">
      <div>
        <h2 className="font-semibold text-lg">İki adımlı doğrulama</h2>
        <p className="mt-0.5 text-sm text-muted-foreground leading-relaxed">
          Girişte parolanızdan sonra telefonunuzdaki doğrulama uygulamasının ürettiği kod istenir. Parolanız
          başkasının eline geçse bile hesabınıza giremez.
          {user.linkedProviders.length ? " Kimlik sağlayıcısıyla girişte de kod sorulur." : ""}
        </p>
      </div>
      {codes ? (
        <RecoveryCodes codes={codes} onDone={() => setCodes(undefined)} />
      ) : user.twoFactorEnabled ? (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
            <div className="flex items-center gap-3">
              <ShieldCheckIcon className="size-5 shrink-0 text-brand-ink" />
              <div>
                <p className="font-medium text-md">Açık</p>
                <p className="text-sm text-muted-foreground">
                  {user.recoveryCodesLeft > 0
                    ? `${user.recoveryCodesLeft} kurtarma kodu kaldı`
                    : "Kurtarma kodunuz kalmadı"}
                </p>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={() => setConfirm("regenerate")}>
                Yeni kurtarma kodları
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="text-destructive"
                onClick={() => setConfirm("disable")}
              >
                Kapat
              </Button>
            </div>
          </div>
          {user.recoveryCodesLeft <= 3 ? (
            <p className="flex gap-2 text-sm text-muted-foreground">
              <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-(--status-warning-solid)" />
              Kurtarma kodlarınız azaldı. Telefonunuzu kaybederseniz giriş yapabilmek için yeni kodlar
              oluşturun.
            </p>
          ) : null}
        </>
      ) : (
        <>
          {requiring.length ? (
            <p className="flex gap-2 rounded-lg border border-(--status-warning-border) bg-(--status-warning-bg) px-4 py-3 text-(--status-warning-fg) text-sm">
              <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-(--status-warning-solid)" />
              {requiring.join(", ")} organizasyonu iki adımlı doğrulama istiyor; açana kadar erişemezsiniz.
            </p>
          ) : null}
          <TwoFactorSetup
            onEnabled={(c) => {
              setCodes(c);
              refresh();
              toast.success("İki adımlı doğrulama açıldı. Diğer cihazlardaki oturumlar kapatıldı.");
            }}
          />
        </>
      )}
      <ConfirmDialog
        mode={confirm}
        requiring={requiring}
        onClose={() => setConfirm(undefined)}
        onCodes={setCodes}
        onChanged={refresh}
      />
    </div>
  );
}

/** Asks for a current code before turning off or renewing recovery codes. */
function ConfirmDialog({
  mode,
  requiring,
  onClose,
  onCodes,
  onChanged,
}: {
  mode?: Confirm;
  requiring: string[];
  onClose: () => void;
  onCodes: (codes: string[]) => void;
  onChanged: () => void;
}) {
  const [code, setCode] = useState("");
  const close = () => {
    setCode("");
    disable.reset();
    regenerate.reset();
    onClose();
  };
  const disable = useMutation(UserService.method.disableTwoFactor, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("İki adımlı doğrulama kapatıldı. Diğer cihazlardaki oturumlar kapatıldı.");
      onChanged();
      close();
    },
  });
  const regenerate = useMutation(UserService.method.regenerateRecoveryCodes, {
    transport: baseTransport,
    onSuccess: (res) => {
      onCodes(res.recoveryCodes);
      onChanged();
      close();
    },
  });
  const pending = disable.isPending || regenerate.isPending;
  const error = disable.error ?? regenerate.error;
  return (
    <Dialog open={mode !== undefined} onOpenChange={(v) => (v ? undefined : close())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {mode === "disable" ? "İki adımlı doğrulamayı kapat" : "Yeni kurtarma kodları oluştur"}
          </DialogTitle>
          <DialogDescription>
            {mode === "disable"
              ? "Onaylamak için doğrulama uygulamanızdaki kodu ya da kurtarma kodlarınızdan birini girin."
              : "Eski kurtarma kodları geçersiz olur. Onaylamak için doğrulama uygulamanızdaki kodu girin."}
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            const m = { code: code.trim() };
            if (mode === "disable") disable.mutate(m);
            else regenerate.mutate(m);
          }}
        >
          {mode === "disable" && requiring.length ? (
            <p className="flex gap-2 rounded-md border border-(--status-warning-border) bg-(--status-warning-bg) px-3 py-2 text-(--status-warning-fg) text-sm">
              <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-(--status-warning-solid)" />
              {requiring.join(", ")} organizasyonu iki adımlı doğrulama istiyor. Kapatırsanız yeniden açana
              kadar erişemezsiniz.
            </p>
          ) : null}
          <Field label="Kod" htmlFor="tf-confirm-code">
            <CodeInput id="tf-confirm-code" value={code} onChange={setCode} recovery autoFocus />
          </Field>
          <FormError message={error ? errorMessage(error) : undefined} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={close}>
              Vazgeç
            </Button>
            <Button type="submit" variant={mode === "disable" ? "destructive" : "default"} disabled={pending}>
              {mode === "disable" ? "Kapat" : "Kodları oluştur"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
