import type { MetadataRoute } from "next";

// Internal deployment: nothing under this origin is meant to be indexed, and
// the fork keeps no public marketing copy to advertise.
export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      {
        userAgent: "*",
        disallow: "/",
      },
    ],
  };
}
