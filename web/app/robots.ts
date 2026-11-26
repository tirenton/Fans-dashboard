// /robots.txt: search engines may read the public pages (Main dashboard, log in, sign up), not the locked ones,
// the one-time email-link pages or the APIs. The proxy lets this through without a login (proxy.ts matcher).
import type { MetadataRoute } from "next";
import { SITE_URL } from "@/lib/site";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [{
      userAgent: "*",
      allow: "/",
      disallow: ["/api/", "/img/", "/feed", "/admin", "/confirm", "/reset", "/forgot"],
    }],
    sitemap: `${SITE_URL}/sitemap.xml`,
  };
}
