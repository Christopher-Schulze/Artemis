# Release Procedures

This document covers the operator-gated procedures for signing, rolling back,
revoking, and responding to vulnerabilities in Artemis releases.

## Artifact Generation

The release generator never builds, signs, tags, uploads or publishes a product release. It seals already-built deliverables into a fresh directory outside the source Git worktree. Required inputs are the clean committed source root, its exact full HEAD, semantic version, `release_hardened` profile, commit-derived source-date epoch, supported target, trusted `sha256:<64 lowercase hex>` toolchain digest, and at least one explicit `name=path` deliverable.

The standalone split is a plain source tree. Initialize and commit it before invoking the release generator so Git identity and committed-tree provenance exist. Build deliverables outside that worktree, then generate the release set into another fresh sibling path:

```bash
codebase/scripts/build/split-artemis.sh /tmp/artemis-source
cd /tmp/artemis-source
git init
git add -A
git commit -m "Artemis release source"

mkdir /tmp/artemis-build
go build -trimpath -buildvcs=false -o /tmp/artemis-build/artemis ./cmd/artemis

ARTEMIS_COMMIT="$(git rev-parse HEAD)"
ARTEMIS_SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
go run ./cmd/artemis-release \
  --source-root . \
  --output /tmp/artemis-release-set \
  --version v0.1.0 \
  --commit "$ARTEMIS_COMMIT" \
  --build-profile release_hardened \
  --source-date-epoch "$ARTEMIS_SOURCE_DATE_EPOCH" \
  --target darwin/arm64 \
  --toolchain-digest "$ARTEMIS_TOOLCHAIN_DIGEST" \
  --artifact artemis=/tmp/artemis-build/artemis
```

`ARTEMIS_TOOLCHAIN_DIGEST` comes from the trusted pinned toolchain attestation, not from the source tree or release generator. The supplied source-date epoch must equal the HEAD committer timestamp. Repeating the command from identical committed inputs with a different fresh output path produces a byte-identical set. The generator creates `artifacts/`, `licenses/`, `checksums.txt`, `license-report.json`, `sbom.cdx.json`, then writes `release-manifest.json` last; it verifies the entire staged set before one exclusive atomic rename and verifies it again after publication. Existing destinations are never deleted or overwritten.

## Signing

### Tag Signing

Release tags are signed by the operator using a GPG key or SSH key
registered with GitHub. The signing key is operator-controlled and never
automated by CI or agents.

```bash
# Create a signed tag
git tag -s v0.1.0 -m "Artemis v0.1.0"

# Verify a signed tag
git tag -v v0.1.0
```

### Artifact Signing

Release artifacts (binaries, checksums, SBOM, license report and manifest) are signed with the operator's
GPG key. The signature is published alongside the artifacts.

```bash
# Sign the checksums file
gpg --detach-sign --armor /tmp/artemis-release-set/checksums.txt

# Verify the signature
gpg --verify /tmp/artemis-release-set/checksums.txt.asc /tmp/artemis-release-set/checksums.txt
```

### Provenance Attestation

The release manifest (`release-manifest.json`) includes the commit hash, Go
version, OS, architecture, and checksums. The operator signs the manifest
and publishes the signature:

```bash
gpg --detach-sign --armor /tmp/artemis-release-set/release-manifest.json
```

## Rollback

If a release is found to be defective or vulnerable:

1. **Stop promotion**: halt any in-progress release announcements and
   downloads.
2. **Yank the tag**: mark the GitHub release as a pre-release or draft, and
   add a prominent warning to the release notes.
3. **Publish a patched release**: cut a new tag (e.g., `v0.1.1`) with the
   fix, produce new artifacts, sign them, and publish.
4. **Update the changelog**: document the rollback in `CHANGELOG.md` under a
   new `### Security` or `### Bug Fixes` heading.
5. **Notify users**: update the README and SECURITY advisory with
   upgrade instructions.

The previous good release tag remains available; users can pin to it while
the patched release is validated.

## Revocation

If a signing key is compromised:

1. **Revoke the key**: publish a revocation certificate for the GPG key.
2. **Re-sign all current artifacts**: produce new signatures with a new key.
3. **Publish a security advisory**: document the compromise, the affected
   releases, and the new signing key fingerprint.
4. **Update trust anchors**: update the project documentation with the new
   key fingerprint.

If a release itself must be revoked (critical vulnerability with no patch
available):

1. Mark the GitHub release as a draft (removes it from public listings).
2. Publish a SECURITY advisory with the CVE and workaround.
3. Pin the `latest` pointer to the previous good release.

## Vulnerability Response

See `SECURITY.md` for the full vulnerability reporting and response
procedure. The summary:

1. **Triage** within 72 hours of report.
2. **Develop fix** on a private branch.
3. **Coordinate disclosure** timing with the reporter (default 90 days).
4. **Publish patched release** with signed artifacts and changelog entry.
5. **Publish public advisory** with credit (unless reporter requests
   anonymity).

## Operator Gate

All of the above procedures require explicit operator action. No agent, CI
job, or automated process may create signed tags, publish releases, revoke
keys, or post security advisories without operator confirmation. The
`artemis-release` tool produces artifacts but does NOT sign or publish them;
signing and publishing are manual operator steps.
