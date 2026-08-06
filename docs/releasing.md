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

## Unsigned dual-builder rehearsal

The application module authorizes an exact central renderer through its
`go.mod` tool directive. `make release-parity` runs that fully qualified tool
and the retained repository builder twice each with `GOWORK=off`,
`GOPROXY=off`, `GOTOOLCHAIN=local`, and `GOFLAGS=-mod=vendor`. It first asks
the central tool for a read-only plan, then renders the plan without resolving
an ambient workspace or downloading a module.

The central renderer is the migration candidate. The retained repository
builder remains both the parity oracle and the production signer:

```text
make release-parity
```

Both rehearsals are unsigned and always archive `HEAD`, never working-tree
contents. Their source archives must be byte-identical and each builder must be
byte-deterministic across two runs. Parity also decodes, bounds, and completely
drains both PAX/gzip streams; hidden decompressed data, an additional gzip
member, compressed trailing bytes, unsafe roots, duplicate entries, or
unsupported metadata fail closed. Each canonical checksum file must verify its
own archive and SBOM, and neither output may contain a signature or public key.

The SBOMs must be semantically identical except for these intentional builder
provenance fields:

- document name (`starter-websocket VERSION` centrally and
  `Spice WebSocket starter VERSION` in the retained builder);
- document namespace shape (the central renderer includes `spdx/v1/`);
- organization and tool creators identifying the actual renderer.

The central renderer uses `Organization: Spice Framework`; the retained
builder uses its existing `Organization: Spice Authors` identity. Package
facts, relationships including `DESCRIBES`, creation time, SPDX contract, and
every other decoded field must match exactly. Because the SBOM bytes differ,
the checksum files differ only in the SBOM digest; the source archive checksum
is identical. The parity gate fails closed on any extra artifact, dependency
drift, malformed or noncanonical checksum, or undocumented SBOM difference.

`make verify-release` executes this dual-builder rehearsal after the complete
repository verification contract. The tag workflow and production ceremony
above deliberately continue to invoke `cmd/starter-websocket-release` and emit
its signed artifacts until signing authority is migrated in a separate review.
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
