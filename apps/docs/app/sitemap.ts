import type { MetadataRoute } from "next";

// Internal deployment: every page is behind the company network and the
// robots policy disallows the whole origin, so the sitemap stays empty
// instead of publishing external URLs.
export default function sitemap(): MetadataRoute.Sitemap {
  return [];
}
