import type { MetadataRoute } from "next";

// Internal deployment: the docs site is not meant to be indexed.
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
