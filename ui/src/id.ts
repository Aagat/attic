// getRandomValues is available on HTTP LAN origins as well as HTTPS.
// These opaque IDs identify save attempts; they are not authentication tokens.
export function randomId(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}
