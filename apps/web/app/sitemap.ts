import type { MetadataRoute } from "next";

// Internal deployment: nothing here should be advertised to crawlers, and the
// marketing pages are kept reachable but unlisted. robots.txt disallows the
// whole origin, so the sitemap stays empty instead of publishing external URLs.
export default function sitemap(): MetadataRoute.Sitemap {
  return [];
}
