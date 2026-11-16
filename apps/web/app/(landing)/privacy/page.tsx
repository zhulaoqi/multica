import type { Metadata } from "next";
import { PrivacyPageClient } from "@/features/landing/components/privacy-page-client";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description:
    "How yiyan collects, uses, and protects personal information on multica.ai and yiyan Cloud.",
  openGraph: {
    title: "Privacy Policy — yiyan",
    description:
      "How yiyan collects, uses, and protects personal information.",
    url: "/privacy",
  },
  alternates: {
    canonical: "/privacy",
  },
};

export default function PrivacyPage() {
  return <PrivacyPageClient />;
}
