import { BoxesIcon, BugIcon, Building2Icon, CompassIcon, type LucideIcon, RadioIcon } from "lucide-react";

export type NavItem = {
  label: string;
  to: "/o/$org" | "/o/$org/issues" | "/o/$org/customers" | "/o/$org/live" | "/o/$org/projects";
  icon: LucideIcon;
  exact?: boolean;
};

/** Primary navigation of an organization. */
export const primaryNav: NavItem[] = [
  { label: "Genel bakış", to: "/o/$org", icon: CompassIcon, exact: true },
  { label: "Hatalar", to: "/o/$org/issues", icon: BugIcon },
  { label: "Müşteriler", to: "/o/$org/customers", icon: Building2Icon },
  { label: "Canlı akış", to: "/o/$org/live", icon: RadioIcon },
  { label: "Projeler", to: "/o/$org/projects", icon: BoxesIcon },
];
