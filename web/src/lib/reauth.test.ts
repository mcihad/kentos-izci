import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, describe, expect, it } from "vitest";
import { SessionSchema } from "@/gen/izci/api/v1/auth_pb";
import { ErrorDetailSchema, ErrorReason } from "@/gen/izci/api/v1/common_pb";
import { recentlyAuthenticated, withReauth } from "./reauth";
import { finishReauth, registerReauthDialog, requestReauth } from "./reauth-bus";

const reauthRequired = () =>
  new ConnectError("confirm your identity", Code.FailedPrecondition, undefined, [
    { desc: ErrorDetailSchema, value: { reason: ErrorReason.REAUTHENTICATION_REQUIRED } },
  ]);

/** A dialog that answers every request with the given verdict. */
function answer(ok: boolean) {
  let shown = 0;
  const unregister = registerReauthDialog(() => {
    shown++;
    queueMicrotask(() => finishReauth(ok));
  });
  return { shown: () => shown, unregister };
}

let cleanup: (() => void) | undefined;
afterEach(() => cleanup?.());

describe("withReauth", () => {
  it("retries once after the user confirms", async () => {
    const dialog = answer(true);
    cleanup = dialog.unregister;
    let calls = 0;
    const result = await withReauth(async () => {
      calls++;
      if (calls === 1) throw reauthRequired();
      return "ok";
    });
    expect(result).toBe("ok");
    expect(calls).toBe(2);
    expect(dialog.shown()).toBe(1);
  });

  it("gives up when the user cancels", async () => {
    const dialog = answer(false);
    cleanup = dialog.unregister;
    let calls = 0;
    const call = async () => {
      calls++;
      throw reauthRequired();
    };
    await expect(withReauth(call)).rejects.toThrow("confirm your identity");
    expect(calls).toBe(1);
  });

  it("passes other errors through without asking", async () => {
    const dialog = answer(true);
    cleanup = dialog.unregister;
    await expect(
      withReauth(async () => {
        throw new ConnectError("nope", Code.PermissionDenied);
      }),
    ).rejects.toThrow("nope");
    expect(dialog.shown()).toBe(0);
  });
});

describe("requestReauth", () => {
  it("shares one dialog between concurrent requests", async () => {
    let shown = 0;
    cleanup = registerReauthDialog(() => shown++);
    const a = requestReauth();
    const b = requestReauth();
    finishReauth(true);
    expect(await a).toBe(true);
    expect(await b).toBe(true);
    expect(shown).toBe(1);
  });

  it("resolves to false without a dialog", async () => {
    expect(await requestReauth()).toBe(false);
  });
});

describe("recentlyAuthenticated", () => {
  const session = (agoMs: number) =>
    create(SessionSchema, { reauthenticatedAt: timestampFromMs(Date.now() - agoMs) });
  it("trusts the last minutes only", () => {
    expect(recentlyAuthenticated(session(60_000))).toBe(true);
    expect(recentlyAuthenticated(session(9.5 * 60_000))).toBe(false);
    expect(recentlyAuthenticated(create(SessionSchema))).toBe(false);
    expect(recentlyAuthenticated(undefined)).toBe(false);
  });
});
