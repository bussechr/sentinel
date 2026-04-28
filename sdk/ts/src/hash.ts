// SHA-256 helpers using Web Crypto so the SDK runs in Node 20+, Bun,
// Cloudflare Workers, Deno, and modern browsers without polyfills.

const subtle: SubtleCrypto =
  (globalThis.crypto && globalThis.crypto.subtle) ||
  (() => {
    throw new Error(
      "sentinel: Web Crypto subtle is required (Node 20+, Bun, Workers, Deno)"
    );
  })();

const enc = new TextEncoder();

function hex(bytes: ArrayBuffer): string {
  const u8 = new Uint8Array(bytes);
  let out = "";
  for (let i = 0; i < u8.length; i++) {
    const v = u8[i] ?? 0;
    out += v.toString(16).padStart(2, "0");
  }
  return out;
}

export async function sha256Hex(value: string | Uint8Array): Promise<string> {
  const bytes =
    typeof value === "string"
      ? enc.encode(value)
      : new Uint8Array(value); // detach SharedArrayBuffer-backed views
  const digest = await subtle.digest("SHA-256", bytes);
  return "sha256:" + hex(digest);
}
