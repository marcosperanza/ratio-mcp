# Synthetic encrypted data

This directory contains synthetic data only. `fixtures/repository.json` uses the
same AES-256-GCM layout as Ratio transactions: ciphertext and the 12-byte IV are
stored as separate Base64 strings, and the authentication tag is appended to the
ciphertext by Web Crypto.

Regenerate both the repository and the interoperability vector with Node.js:

```bash
node testdata/generate.mjs
```

The fixed key and IV values are intentionally public and deterministic. They are
test material and must never be reused for real data.
