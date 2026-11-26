// The web app manifest (/manifest.webmanifest): the name and icon when the site is installed or added to a home
// screen, and the name browsers show for the site instead of its address.
import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "AxisMixx · NMIXX FANS Statistics",
    short_name: "AxisMixx",
    description: "When NMIXX members like and comment on fan posts on FANS, and when fans post.",
    start_url: "/",
    display: "standalone",
    background_color: "#0d0d0e",
    theme_color: "#0d0d0e",
    icons: [
      { src: "/icon.svg", sizes: "any", type: "image/svg+xml" },
      { src: "/apple-icon.png", sizes: "180x180", type: "image/png" },
    ],
  };
}
