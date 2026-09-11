# Artemis Production Readiness Seal — Evidence Matrix

This document maps every TASK-2348 through TASK-2361 capability to its
production entrypoint, real test, failure test, platform result, documentation
state, and release artifact. It is the finite acceptance evidence for
TASK-2362.

## Seal Scope

This is the Artemis FUNCTIONAL + baseline-security seal only. The deep
network-security hardening (Chromium/CDP egress, Serve auth/ownership,
downloads, per-session budgets, WebSocket policy, audit) was descoped from
TASK-2356 into TASK-2356-A..G and is sealed by their terminal gate
TASK-2356-G plus the program final seal TASK-2389, which run later.

## Evidence Matrix

| Capability | Entrypoint | Real Test | Failure Test | Platform | Docs | Artifact |
|---|---|---|---|---|---|---|
| Renderless fetch + JS execution | Agent API `FetchAction` | `TestAgentExecutesFetchWithObservableEvidence` | `TestRenderlessDispatcherRejectsInvalidVariantsAndRuntimeFailures` | macOS arm64, Linux x86_64 | `docs/documentation.md` | `checksums.txt` |
| Agent lifecycle (start/stop/recovery) | Agent API | `TestAgentLifecycleTransitionsAndIdempotentStop`, `TestAgentCanRecoverFromFailedStart` | `TestAgentStartRollsBackPartialRuntime`, `TestAgentStartRejectsNilRuntime` | macOS arm64, Linux x86_64 | `docs/documentation.md` | `checksums.txt` |
| Task JSON wire protocol | Agent API `Task` | `TestTaskJSONRoundTripPreservesTypedAction` | `TestTaskErrorSupportsErrorsIsAndCauseFreeFormatting` | macOS arm64, Linux x86_64 | `docs/documentation.md` | `checksums.txt` |
| Session management | Agent API `Session` | `TestSessionCloseCancelsAndWaitsForOwnedExecution` | `TestAgentRejectsNilExecutionContext` | macOS arm64, Linux x86_64 | `docs/documentation.md` | `checksums.txt` |
| Timeout and cancellation | Agent API `Task.Timeout` | `TestAgentTimeoutAndStableInjectedErrorClasses`, `TestAgentAppliesConfiguredOperationTimeouts` | `TestAgentRejectsNegativeTaskTimeout` | macOS arm64, Linux x86_64 | `docs/documentation.md` | `checksums.txt` |
| Capability registry | `Capabilities()` | `TestSupportedCapabilitiesNameBehaviorEvidence` | `TestSealCapabilityRegistryHasNoUnsupportedSupportedEntries` | macOS arm64, Linux x86_64 | `README.md` | `checksums.txt` |
| Serve (WebSocket steering) | `cmd/artemis serve` | `TestServeStartupAndShutdown` | `serve/server_test.go` auth/origin tests | macOS arm64, Linux x86_64 | `docs/documentation.md` | `checksums.txt` |
| CLI doctor | `cmd/artemis doctor` | `TestDoctorJSONExitCode`, `TestDoctorTextExitCode`, `TestDoctorVerifiesPlatform` | — | macOS arm64, Linux x86_64 | `README.md` | `checksums.txt` |
| CLI trace | `cmd/artemis trace` | `TestTraceEmitsJSON` | — | macOS arm64, Linux x86_64 | `README.md` | `checksums.txt` |
| CLI profile CRUD | `cmd/artemis profile` | `TestProfileCRUD` | `TestProfileRequiresOwner` | macOS arm64, Linux x86_64 | `README.md` | `checksums.txt` |
| CLI benchmark | `cmd/artemis benchmark` | `TestBenchmarkArtemisOnly` | — | macOS arm64, Linux x86_64 | `docs/documentation.md` | `scorecard.json` |
| Network policy (SSRF deny) | `network.Policy` | `TestSealEngineSecureByDefault` | `network/ipfilter.go` tests | macOS arm64, Linux x86_64 | `SECURITY.md` | `checksums.txt` |
| Benchmark harness (fail-closed) | `benchmark.Harness` | `TestSealBenchmarkHarnessIsFailClosed` | `benchmark/harness_test.go` | macOS arm64, Linux x86_64 | `docs/documentation.md` | `scorecard.json` |
| V8 provenance | `third_party/v8go/` | `TestProvenanceDocument`, `TestLicenseExists` | — | macOS arm64, Linux x86_64 | `third_party/v8go/PROVENANCE.md` | `sbom.cdx.json` |
| Release artifacts | `cmd/artemis-release` | `TestBuildReleaseArtifactSetIsByteReproducible`, `TestCurrentModuleGraphAndLicenseEvidenceAreComplete`, `TestRunCLIPublishesReleaseSet`, `TestEmbeddedCycloneDXSchemaPin` | `TestBuildReleaseArtifactSetRejectsInvalidInputsWithoutPublication`, `TestBuildReleaseArtifactSetFailsClosedAcrossPipelineBoundaries`, `TestReleaseArtifactVerificationRejectsTamperingAndUnknownJSON`, `TestRenameExclusiveRejectsExistingDestination`, `TestCycloneDXSchemaRejectsInvalidDocuments` | macOS arm64, Linux x86_64 | `docs/release-procedures.md` | `release-manifest.json`, `checksums.txt`, `sbom.cdx.json`, `license-report.json` |
| Project hygiene | Public repo files | `TestProjectHygieneFiles`, `TestGitignoreCoversBuildArtifacts` | — | macOS arm64, Linux x86_64 | `SECURITY.md`, `CONTRIBUTING.md`, `CHANGELOG.md` | `license-report.json` |
| Split verification | `split-artemis.sh` | `TestSplitArtemisPublishesValidatedStandaloneTree`, `TestSplitArtemisScriptExists` | `TestSplitArtemisRejectsExistingAndSourceDestinations`, `TestSplitArtemisScriptIsSyntaxValidAndNeverDeletesOutput`; the real fixture also rejects untracked Artemis source | macOS arm64, Linux x86_64 | `CONTRIBUTING.md` | Exact Git-tree standalone source publication |
| Release identity | `Version`, `LICENSE`, `go.mod` | `TestReleaseIdentityAndClaimsDoNotDrift`, `TestSealVersionAndLicenseConsistency` | — | macOS arm64, Linux x86_64 | `README.md` | `release-manifest.json` |
| No synthetic success | Agent API | `TestSealNoSyntheticSuccessInAgent` | `TestRenderlessDispatcherRejectsInvalidVariantsAndRuntimeFailures` | macOS arm64, Linux x86_64 | `docs/documentation.md` | — |

## Seal Invariants Verified

1. **engine.Config{} denies private networks** — `TestSealEngineSecureByDefault`
2. **No synthetic/no-op tool responses** — `TestSealNoSyntheticSuccessInAgent`
3. **Serve/CLI + Agent protocol works** — `TestServeStartupAndShutdown`, `TestDoctorVerifiesPlatform`, `TestAgentExecutesFetchWithObservableEvidence`
4. **Release artifacts exist** — `TestSealReleaseArtifactGeneratorExists`, `TestBuildReleaseArtifactSetIsByteReproducible`
5. **Zero committed build artifacts** — `TestSealNoCommittedBuildArtifacts`, `TestGitignoreCoversBuildArtifacts`
6. **Cross-platform CI** — `TestSealSupplyChainArtifactsExist` verifies CI matrix covers Linux + macOS
7. **V8 provenance documented** — `TestProvenanceDocument`, `TestSealSupplyChainArtifactsExist`
8. **Benchmark harness is fail-closed** — `TestSealBenchmarkHarnessIsFailClosed`
9. **Version/license/module identity consistent** — `TestSealVersionAndLicenseConsistency`, `TestReleaseIdentityAndClaimsDoNotDrift`
10. **Capability registry has evidence** — `TestSealCapabilityRegistryHasNoUnsupportedSupportedEntries`, `TestSupportedCapabilitiesNameBehaviorEvidence`

## Operator Release Command

The operator-controlled tag and release command (NOT executed by this TASK):

```bash
# 1. Verify clean tree
git status --short --branch -uall

# 2. Run the split
codebase/scripts/build/split-artemis.sh /tmp/artemis-split

# 3. Initialize the plain split as a clean committed source repository
cd /tmp/artemis-split && git init && git add -A && git commit -m "Artemis release source"

# 4. Build outside the source and seal the exact deliverable set
mkdir /tmp/artemis-build
go build -trimpath -buildvcs=false -o /tmp/artemis-build/artemis ./cmd/artemis
ARTEMIS_COMMIT="$(git rev-parse HEAD)"
ARTEMIS_SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
go run ./cmd/artemis-release --source-root . --output /tmp/artemis-release-set --version v0.1.0 --commit "$ARTEMIS_COMMIT" --build-profile release_hardened --source-date-epoch "$ARTEMIS_SOURCE_DATE_EPOCH" --target darwin/arm64 --toolchain-digest "$ARTEMIS_TOOLCHAIN_DIGEST" --artifact artemis=/tmp/artemis-build/artemis

# 5. Sign the tag (operator GPG key)
git tag -s v0.1.0 -m "Artemis v0.1.0"

# 6. Sign the artifacts
gpg --detach-sign --armor /tmp/artemis-release-set/checksums.txt
gpg --detach-sign --armor /tmp/artemis-release-set/release-manifest.json

# 7. Push (operator decision)
git push origin main --tags
```
