import type { Metadata } from "next";
import { AboutPageClient } from "@/features/landing/components/about-page-client";

export const metadata: Metadata = {
  title: "About",
  description:
    "Learn about 易研 — multiplexed information and computing agent. A source-available project management platform for human + agent teams.",
  openGraph: {
    title: "About 易研",
    description:
      "The story behind 易研 and why we're building project management for human + agent teams.",
    url: "/about",
  },
  alternates: {
    canonical: "/about",
  },
};

export default function AboutPage() {
  return <AboutPageClient />;
}
