import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { UserPlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { SecretValue } from "@/components/common/copy-button";
import { Field, FormError } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { UserAvatar } from "@/components/common/user-avatar";
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
import { Skeleton } from "@/components/ui/skeleton";
import type { Invitation } from "@/gen/izci/api/v1/auth_pb";
import { Role } from "@/gen/izci/api/v1/common_pb";
import { type Member, OrganizationService } from "@/gen/izci/api/v1/organization_pb";
import { errorMessage } from "@/lib/errors";
import { roleLabels } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { useSession } from "@/lib/session";

const roles = [Role.VIEWER, Role.MEMBER, Role.ADMIN, Role.OWNER];
const roleHints: Record<number, string> = {
  [Role.VIEWER]: "Hataları görür, değiştiremez",
  [Role.MEMBER]: "Hataları çözer, atar, yorum yapar",
  [Role.ADMIN]: "Projeleri, anahtarları ve üyeleri yönetir",
  [Role.OWNER]: "Organizasyonun tamamı, silme dahil",
};

function useInvalidateOrg() {
  const org = useOrg();
  const qc = useQueryClient();
  return () =>
    qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: OrganizationService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
}

export function Members() {
  const me = useSession()?.user?.id;
  const org = useOrg();
  const members = useQuery(OrganizationService.method.listMembers, {});
  const invalidate = useInvalidateOrg();
  const [inviting, setInviting] = useState(false);
  const [removing, setRemoving] = useState<Member>();
  const done = (msg: string) => () => {
    toast.success(msg);
    void invalidate();
  };
  const fail = (err: unknown) => toast.error(errorMessage(err));
  const update = useMutation(OrganizationService.method.updateMember, {
    onSuccess: done("Rol güncellendi"),
    onError: fail,
  });
  const remove = useMutation(OrganizationService.method.removeMember, {
    onSuccess: () => {
      setRemoving(undefined);
      done("Üye çıkarıldı")();
    },
    onError: fail,
  });
  const revoke = useMutation(OrganizationService.method.revokeInvitation, {
    onSuccess: done("Davet geri alındı"),
    onError: fail,
  });
  const owner = org.role === Role.OWNER;

  if (!members.data) return <Skeleton className="my-6 h-48 w-full max-w-4xl" />;
  return (
    <div className="grid max-w-4xl grid-cols-1 *:min-w-0 gap-8 py-6">
      <section>
        <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="font-semibold text-lg">Üyeler</h2>
            <p className="mt-0.5 text-sm text-muted-foreground">
              {members.data.members.length} kişi bu organizasyona erişebiliyor.
            </p>
          </div>
          <Button size="sm" onClick={() => setInviting(true)}>
            <UserPlusIcon className="size-4" />
            Üye davet et
          </Button>
        </div>
        <ul className="divide-y rounded-lg border">
          {members.data.members.map((m) => {
            const self = m.user?.id === me;
            // Your own membership, and an owner's when you are not one,
            // cannot be changed here: show the role instead of dead controls.
            const locked = self || (!owner && m.role === Role.OWNER);
            return (
              <li key={m.user?.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <UserAvatar
                  name={m.user?.name || m.user?.email || ""}
                  src={m.user?.avatarUrl}
                  className="size-8"
                />
                <div className="min-w-0 flex-[1_1_14rem]">
                  <p className="truncate font-medium text-md">
                    {m.user?.name || m.user?.email}
                    {self ? (
                      <span className="ml-2 font-normal text-muted-foreground text-sm">siz</span>
                    ) : null}
                  </p>
                  <p className="truncate text-sm text-muted-foreground">
                    {m.user?.email}
                    {m.lastSeenAt ? (
                      <>
                        {", son etkinlik "}
                        <RelativeTime value={m.lastSeenAt} />
                      </>
                    ) : null}
                  </p>
                </div>
                {locked ? (
                  <span className="text-sm text-muted-foreground max-sm:ml-11 sm:pr-3">
                    {roleLabels[m.role]}
                  </span>
                ) : (
                  <>
                    <Select
                      value={String(m.role)}
                      disabled={update.isPending}
                      onValueChange={(v) =>
                        update.mutate({ userId: m.user?.id ?? "", role: Number(v) as Role })
                      }
                    >
                      <SelectTrigger size="sm" className="w-32 text-sm max-sm:ml-11" aria-label="Rol">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent align="end">
                        {roles
                          .filter((r) => owner || r !== Role.OWNER)
                          .map((r) => (
                            <SelectItem key={r} value={String(r)}>
                              {roleLabels[r]}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-destructive"
                      onClick={() => setRemoving(m)}
                    >
                      Çıkar
                    </Button>
                  </>
                )}
              </li>
            );
          })}
        </ul>
      </section>
      {members.data.invitations.length > 0 ? (
        <section>
          <h2 className="mb-3 font-semibold text-lg">Bekleyen davetler</h2>
          <ul className="divide-y rounded-lg border">
            {members.data.invitations.map((i) => (
              <li key={i.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-md">{i.email}</p>
                  <p className="text-sm text-muted-foreground">
                    {roleLabels[i.role]}, <RelativeTime value={i.expiresAt} /> sona eriyor
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={revoke.isPending}
                  onClick={() => revoke.mutate({ invitationId: i.id })}
                >
                  Geri al
                </Button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      <InviteDialog open={inviting} onOpenChange={setInviting} owner={owner} />
      <Dialog open={!!removing} onOpenChange={(v) => !v && setRemoving(undefined)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Üyeyi çıkar</DialogTitle>
            <DialogDescription>
              {removing?.user?.name || removing?.user?.email} bu organizasyona artık erişemeyecek. Atanmış
              hataları sahipsiz kalır.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setRemoving(undefined)}>
              Vazgeç
            </Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => removing && remove.mutate({ userId: removing.user?.id ?? "" })}
            >
              Çıkar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function InviteDialog({
  open,
  onOpenChange,
  owner,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  owner: boolean;
}) {
  const invalidate = useInvalidateOrg();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState(Role.MEMBER);
  const [created, setCreated] = useState<Invitation>();
  const invite = useMutation(OrganizationService.method.inviteMember, {
    onSuccess: (res) => {
      setCreated(res.invitation);
      void invalidate();
    },
  });
  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v) {
      setCreated(undefined);
      setEmail("");
      invite.reset();
    }
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Davet hazır</DialogTitle>
              <DialogDescription>
                Bağlantıyı {created.email} ile paylaşın. Bağlantı <RelativeTime value={created.expiresAt} />{" "}
                geçersiz olur.
              </DialogDescription>
            </DialogHeader>
            <SecretValue value={created.url} />
            <DialogFooter>
              <Button onClick={() => close(false)}>Tamam</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Üye davet et</DialogTitle>
              <DialogDescription>
                Davet bağlantısı oluşturulur; e-posta adresi hesabı eşleştirmek için kullanılır.
              </DialogDescription>
            </DialogHeader>
            <form
              className="grid gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                invite.mutate({ email, role });
              }}
            >
              <Field label="E-posta" htmlFor="invite-email">
                <Input
                  id="invite-email"
                  type="email"
                  required
                  autoFocus
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </Field>
              <Field label="Rol" htmlFor="invite-role" hint={roleHints[role]}>
                <Select value={String(role)} onValueChange={(v) => setRole(Number(v) as Role)}>
                  <SelectTrigger id="invite-role" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {roles
                      .filter((r) => owner || r !== Role.OWNER)
                      .map((r) => (
                        <SelectItem key={r} value={String(r)}>
                          {roleLabels[r]}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
              </Field>
              <FormError message={invite.error ? errorMessage(invite.error) : undefined} />
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => close(false)}>
                  Vazgeç
                </Button>
                <Button type="submit" disabled={invite.isPending}>
                  Davet oluştur
                </Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
