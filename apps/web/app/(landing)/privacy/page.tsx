import type { Metadata } from "next";
import { PrivacyPageClient } from "@/features/landing/components/privacy-page-client";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description:
    "How 易研 collects, uses, and protects personal information on multica.ai and 易研 Cloud.",
  openGraph: {
    title: "Privacy Policy — 易研",
    description:
      "How 易研 collects, uses, and protects personal information.",
    url: "/privacy",
  },
  alternates: {
    canonical: "/privacy",
  },
};

export default function PrivacyPage() {
  return <PrivacyPageClient />;
}
