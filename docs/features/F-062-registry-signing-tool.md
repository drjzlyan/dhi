# F-062 — Publisher tool for signed pack indexes

Status: done (M27 P7; closes the M18 "signature verification deferred" note)

## Finding
The roadmap still listed pack signature verification as deferred, but the
consumer side shipped as an F-034 follow-on (ADR-0022):
- Ed25519 detached signatures over a registry's `index.toml`;
- publisher keys pinned in `.dhi/registry/trusted_keys` (Settings ›
  MARKETPLACE › publisher key);
- with a pinned key, every refresh and every cached read refuses an
  unsigned or tampered index by name.

What was missing was the producer: a publisher had no way to make a key or
a signature without writing Go.

## Behaviour
`scripts/registry-sign` (a dev tool, not part of `dhi`):

| Command | Does |
|---|---|
| `keygen <dir>` | writes `<dir>/publisher.key` (0600, refuses to overwrite) and prints the public key to publish |
| `sign <publisher.key> <index.toml>` | writes `index.toml.sig` beside it, the file the registry looks for |
| `verify <public-key-hex> <index.toml>` | checks a signed index the way DHI will |

Ed25519 over the index is the chosen mechanism (ADR-0022): the digests in
the index pin each pack's content, and the signature pins the index. No
sigstore dependency or network round-trip is needed at verify time, so
verification stays hermetic.

## Acceptance
- [x] keygen → sign → verify passes; a modified index is refused; the key
      file is 0600 (run by hand; the registry's own tests cover the consumer
      policy)
