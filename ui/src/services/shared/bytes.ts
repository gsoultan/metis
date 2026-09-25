/**
 * Text carried in a Go `[]byte` field, which JSON encodes as base64.
 *
 * `btoa` and `atob` work one character per byte, so they only round-trip
 * Latin-1. `btoa` throws on anything past it — a step named "审批" made a model
 * impossible to import — and `atob` hands back UTF-8 bytes as separate
 * characters, which an exported file then wrote out garbled. These go through
 * the UTF-8 bytes, which is what the server stores.
 */

/** A string as the base64 of its UTF-8 bytes. */
export function toBase64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  // In slices: spreading a large model into one fromCharCode call exceeds the
  // engine's argument limit.
  const slice = 0x8000;
  let binary = '';
  for (let start = 0; start < bytes.length; start += slice) {
    binary += String.fromCharCode(...bytes.subarray(start, start + slice));
  }
  return btoa(binary);
}

/** The string whose UTF-8 bytes `encoded` carries. */
export function fromBase64(encoded: string): string {
  const binary = atob(encoded);
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}
