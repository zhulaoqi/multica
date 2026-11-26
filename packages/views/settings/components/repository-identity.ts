/** Default ports the URL parser keeps for schemes it does not know. */
const DEFAULT_PORTS: Record<string, string> = { "ssh:": "22", "git:": "9418" };

/**
 * Host + path identity of a clone URL, so HTTPS and SSH forms of the same
 * repository compare equal. Path casing is preserved: hosts are
 * case-insensitive, repository paths are not guaranteed to be.
 */
export function repositoryIdentity(rawURL: string): string | null {
  const value = rawURL.trim();
  if (!value) return null;

  let host = "";
  let path = "";
  if (!value.includes("://")) {
    const scpLike = value.match(/^(?:[^@\s/]+@)?([^:\s/]+):(.+)$/);
    if (scpLike) {
      host = scpLike[1] ?? "";
      path = scpLike[2] ?? "";
    }
  }
  if (!host) {
    try {
      const parsed = new URL(value);
      host = parsed.hostname;
      if (parsed.port && parsed.port !== DEFAULT_PORTS[parsed.protocol]) {
        host = `${host}:${parsed.port}`;
      }
      path = parsed.pathname;
    } catch {
      return null;
    }
  }

  const normalizedPath = path
    .replace(/^\/+|\/+$/g, "")
    .replace(/\.git$/i, "");
  if (!host || !normalizedPath) return null;
  return `${host.toLowerCase()}/${normalizedPath}`;
}
