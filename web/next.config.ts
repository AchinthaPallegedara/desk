import type { NextConfig } from "next";

// In production Caddy/Nginx routes /api and /mcp to the Go service.
// In dev, Next proxies them so the browser stays same-origin (cookies, no CORS).
const backendURL = process.env.BACKEND_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  // Self-contained server bundle for the VPS (node .next/standalone/server.js).
  output: "standalone",
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${backendURL}/api/:path*` },
      { source: "/mcp", destination: `${backendURL}/mcp` },
      { source: "/oauth/:path*", destination: `${backendURL}/oauth/:path*` },
      { source: "/.well-known/:path*", destination: `${backendURL}/.well-known/:path*` },
    ];
  },
};

export default nextConfig;
