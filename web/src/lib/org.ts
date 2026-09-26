import type { Transport } from "@connectrpc/connect";
import { createContext, useContext } from "react";
import type { Role } from "@/gen/izci/api/v1/common_pb";

export type OrgContextValue = {
  slug: string;
  name: string;
  role: Role;
  transport: Transport;
};

export const OrgContext = createContext<OrgContextValue | null>(null);

/** The organization of the current /o/$org route. */
export function useOrg(): OrgContextValue {
  const v = useContext(OrgContext);
  if (!v) throw new Error("useOrg must be used below an organization route");
  return v;
}
