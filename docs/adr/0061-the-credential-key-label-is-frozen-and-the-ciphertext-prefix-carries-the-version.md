---
status: accepted
date: 2026-11-20
---

# The credential key label is frozen, and the ciphertext prefix carries the version

qui derives the key that encrypts stored credentials from the session secret with HKDF-SHA256. The derivation takes a label, `encryptionKeyInfo` in `internal/config`, which binds the key to this one purpose, so a later key from the same session secret is independent of it.

The label never changes. A different label gives a different key, and each stored credential would then fail to decrypt with no way back. The format version lives in the ciphertext prefix (`qui2:`), so a new format gets a new prefix and a new reader, and the label stays as it is.

## Consequences

- A second key for another purpose takes its own label. It does not reuse or rename this one.
- The name of the constant avoids the substring "cred", because gosec G101 reports it as a hard-coded credential.
