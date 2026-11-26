import {
  BellRingIcon,
  BoxesIcon,
  BugIcon,
  Building2Icon,
  CompassIcon,
  LayoutDashboardIcon,
  type LucideIcon,
  MessageSquareTextIcon,
  RadioIcon,
  SparklesIcon,
  TimerIcon,
} from "lucide-react";

export type NavItem = {
  label: string;
  to:
    | "/o/$org"
    | "/o/$org/issues"
    | "/o/$org/dashboards"
    | "/o/$org/feedback"
    | "/o/$org/customers"
    | "/o/$org/live"
    | "/o/$org/monitors"
    | "/o/$org/projects"
    | "/o/$org/alerts"
    | "/o/$org/ai";
  icon: LucideIcon;
  exact?: boolean;
  /** Sidebar group; items without one sit above the groups. */
  group?: "İzleme" | "Müşteri" | "Yapılandırma";
  /** Extra words the sidebar search matches. */
  keywords?: string[];
};

/** Primary navigation of an organization, in sidebar order. */
export const primaryNav: NavItem[] = [
  { label: "Genel bakış", to: "/o/$org", icon: CompassIcon, exact: true, keywords: ["ana sayfa", "özet"] },
  {
    label: "Hatalar",
    to: "/o/$org/issues",
    icon: BugIcon,
    group: "İzleme",
    keywords: ["issue", "istisna", "exception", "olay"],
  },
  {
    label: "Panolar",
    to: "/o/$org/dashboards",
    icon: LayoutDashboardIcon,
    group: "İzleme",
    keywords: ["dashboard", "grafik", "kayıtlı arama"],
  },
  {
    label: "Canlı akış",
    to: "/o/$org/live",
    icon: RadioIcon,
    group: "İzleme",
    keywords: ["live", "olaylar"],
  },
  {
    label: "Zamanlanmış görevler",
    to: "/o/$org/monitors",
    icon: TimerIcon,
    group: "İzleme",
    keywords: ["cron", "monitor", "check-in"],
  },
  {
    label: "Müşteriler",
    to: "/o/$org/customers",
    icon: Building2Icon,
    group: "Müşteri",
    keywords: ["customer", "sağlık", "rapor"],
  },
  {
    label: "Geri bildirim",
    to: "/o/$org/feedback",
    icon: MessageSquareTextIcon,
    group: "Müşteri",
    keywords: ["feedback", "kullanıcı", "şikâyet"],
  },
  {
    label: "Projeler",
    to: "/o/$org/projects",
    icon: BoxesIcon,
    group: "Yapılandırma",
    keywords: ["DSN", "SDK", "kaynak eşleme"],
  },
  {
    label: "Uyarılar",
    to: "/o/$org/alerts",
    icon: BellRingIcon,
    group: "Yapılandırma",
    keywords: ["alert", "bildirim", "telegram", "e-posta"],
  },
  {
    label: "Yapay zekâ",
    to: "/o/$org/ai",
    icon: SparklesIcon,
    group: "Yapılandırma",
    keywords: ["AI", "çözüm", "düzeltme"],
  },
];
