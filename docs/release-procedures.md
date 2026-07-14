# Release Procedures

This document covers the operator-gated procedures for signing, rolling back,
revoking, and responding to vulnerabilities in Artemis releases.

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

Release artifacts (binaries, checksums, SBOM) are signed with the operator's
GPG key. The signature is published alongside the artifacts.

```bash
# Sign the checksums file
gpg --detach-sign --armor dist/checksums.txt

# Verify the signature
gpg --verify dist/checksums.txt.asc dist/checksums.txt
```

### Provenance Attestation

The release manifest (`release-manifest.json`) includes the commit hash, Go
version, OS, architecture, and checksums. The operator signs the manifest
and publishes the signature:

```bash
gpg --detach-sign --armor dist/release-manifest.json
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
