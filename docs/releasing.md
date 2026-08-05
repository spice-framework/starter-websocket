# Release contract

starter-websocket releases are ordinary Go module tags plus a small, independently
verifiable artifact set. The repository owns the complete build. No external
release build service, mutable workspace snapshot, or network-resolved package
list participates in artifact construction.

For `v1.2.3`, the release builder produces:

| Artifact | Contract |
|---|---|
| `starter-websocket_1.2.3_source.tar.gz` | Exact tagged Git commit, under one versioned directory |
| `starter-websocket_1.2.3_sbom.spdx.json` | SPDX 2.3 packages from the consistent committed `go.mod`, `go.sum`, and `vendor/modules.txt` graph |
| `checksums.txt` | SHA-256 of the source archive and SBOM, sorted by filename |
| `checksums.txt.sig` | Raw Ed25519 signature of the exact checksum bytes |
| `checksums.txt.pem` | X.509 SubjectPublicKeyInfo PEM for signature verification |

The source archive is reconstructed from the full commit's `git ls-tree`
identity and exact object bytes read through `git cat-file --batch`. It never
uses checkout filters or `git archive`, so `core.autocrlf` and host line-ending
settings cannot alter an artifact. Every tar and gzip timestamp is the source
commit epoch; paths are relative, ownership is zeroed, executable modes and
validated symlinks are preserved, and gzip output is deterministic. Gitlinks
and unsupported modes fail closed. Dirty or untracked workspace files cannot
enter the archive. The SBOM creation time uses the same epoch and contains no
absolute checkout path. Construction fails when committed module selection,
checksums, and vendored versions or replacements disagree; the builder does
not rely on an earlier verifier to detect a stale dependency graph.

## Production ceremony

Production mode fails unless all of these conditions hold:

1. `-version` is canonical, v-prefixed SemVer.
2. the checkout is clean, including untracked files;
3. the named tag resolves exactly to `HEAD`;
4. an Ed25519 private key is supplied;
5. any explicit source epoch equals the `HEAD` commit epoch; and
6. the output directory does not already exist.

The tag workflow runs `make verify-release`, materializes the protected
`STARTER_WEBSOCKET_RELEASE_SIGNING_KEY` secret with owner-only permissions, invokes
the repository command, removes the key even after failure, and publishes only
the newly created `dist` files. The workflow never prints the key. Creating or
rotating that repository secret and pushing a release tag are separate human
release-authority actions; no development task should manufacture either.

## Unsigned rehearsal

An explicit rehearsal exercises the exact same source archive, SBOM, and
checksum pipeline without requiring a clean checkout or matching tag:

```text
go run ./cmd/starter-websocket-release \
  -rehearsal \
  -version=v0.0.0-rehearsal \
  -output=dist-rehearsal
```

Rehearsals are always unsigned and always archive `HEAD`, never working-tree
contents. Passing a signing key together with `-rehearsal` is rejected.
`make verify-release` runs two rehearsals and compares every byte after the
complete repository verification contract.

## Consumer verification

With OpenSSL 3 and GNU-compatible checksum tooling:

```text
sha256sum -c checksums.txt
openssl pkeyutl -verify -pubin -inkey checksums.txt.pem \
  -rawin -in checksums.txt -sigfile checksums.txt.sig
```

Until a separately reviewed Ed25519 public-key fingerprint is pinned in this
repository, GitHub's protected `spice-framework/starter-websocket` release channel
and immutable tag are the authenticity anchor. A public key bundled beside its
own signature proves only artifact-set consistency, not independent identity.
Consumers requiring an independent anchor must pin and compare the reviewed
fingerprint before trusting the signature. The project must not describe the
bundled key alone as proof of publisher authenticity.
