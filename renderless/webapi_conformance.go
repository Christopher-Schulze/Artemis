package renderless

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// WebAPIConformanceEvidence links an explicitly scoped classification to current
// executable tests. Passing runs and authenticated receipts remain gate-owned.
type WebAPIConformanceEvidence struct {
	CaseID   string                     `json:"case_id"`
	Category CapabilityCategory         `json:"category"`
	Scope    string                     `json:"scope"`
	Tests    []ConformanceTestReference `json:"tests"`
}

// ConformanceCoverage reports source linkage, not passing runs or semantic parity.
// Unmapped tests cannot substantiate a capability through this registry.
type ConformanceCoverage struct {
	DeclaredTests int                        `json:"declared_tests"`
	MappedTests   int                        `json:"mapped_tests"`
	UnmappedTests []ConformanceTestReference `json:"unmapped_tests"`
}

type conformanceDefinition struct {
	id       string
	category CapabilityCategory
	names    string
	scope    string
	tests    string
}

type compiledConformance struct {
	sourceHash string
	cases      map[string]WebAPIConformanceEvidence
	apiCases   map[string]string
	coverage   ConformanceCoverage
}

var currentConformance = sync.OnceValues(func() (*compiledConformance, error) {
	digest, references, err := loadConformanceSourceReferences()
	if err != nil {
		return nil, err
	}
	return compileConformanceDefinitions(digest, references, conformanceDefinitions())
})

func compileConformanceDefinitions(digest string, references map[string]ConformanceTestReference, definitions []conformanceDefinition) (*compiledConformance, error) {
	result := &compiledConformance{sourceHash: digest, cases: make(map[string]WebAPIConformanceEvidence), apiCases: make(map[string]string)}
	for _, definition := range definitions {
		if _, duplicate := result.cases[definition.id]; duplicate {
			return nil, fmt.Errorf("conformance: duplicate case %s", definition.id)
		}
		evidence, err := compileConformanceDefinition(definition, references)
		if err != nil {
			return nil, err
		}
		result.cases[definition.id] = evidence
		for _, name := range strings.Fields(definition.names) {
			key := webAPIIdentity(name)
			if previous, duplicate := result.apiCases[key]; duplicate && previous != definition.id {
				return nil, fmt.Errorf("conformance: conflicting API ownership %s", name)
			}
			result.apiCases[key] = definition.id
		}
	}
	if _, exists := result.cases["binding-admission"]; !exists {
		return nil, fmt.Errorf("conformance: missing binding admission case")
	}
	result.coverage = conformanceCoverage(references, result.cases)
	return result, nil
}

func conformanceCoverage(references map[string]ConformanceTestReference, cases map[string]WebAPIConformanceEvidence) ConformanceCoverage {
	mapped := make(map[string]bool)
	for _, evidence := range cases {
		for _, reference := range evidence.Tests {
			mapped[reference.Suite+"."+reference.Test] = true
		}
	}
	result := ConformanceCoverage{DeclaredTests: len(references), UnmappedTests: []ConformanceTestReference{}}
	keys := make([]string, 0, len(references))
	for key := range references {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if mapped[key] {
			result.MappedTests++
			continue
		}
		result.UnmappedTests = append(result.UnmappedTests, references[key])
	}
	return result
}

func compileConformanceDefinition(definition conformanceDefinition, references map[string]ConformanceTestReference) (WebAPIConformanceEvidence, error) {
	evidence := WebAPIConformanceEvidence{CaseID: definition.id, Category: definition.category, Scope: definition.scope}
	if definition.id == "" || definition.scope == "" || strings.TrimSpace(definition.tests) == "" {
		return evidence, fmt.Errorf("conformance: incomplete case %s", definition.id)
	}
	switch definition.category {
	case CategorySupportedReal, CategoryStubCompatible, CategoryUnsupportedEscalate:
	default:
		return evidence, fmt.Errorf("conformance: invalid category %s", definition.category)
	}
	for _, key := range strings.Fields(definition.tests) {
		reference, exists := references[key]
		if !exists {
			return evidence, fmt.Errorf("conformance: missing executable test %s for %s", key, definition.id)
		}
		evidence.Tests = append(evidence.Tests, reference)
	}
	return evidence, nil
}

func conformanceDefinitions() []conformanceDefinition {
	return []conformanceDefinition{
		{"binding-admission", CategoryUnsupportedEscalate, "", "Observed presence or reserved unsupported semantics; no real-browser grant", `renderless.TestInstalledRegistryUsesActualSourceBindings renderless.TestBindingLookupResolvesRealAliasesAndPrototypes renderless.TestRegistryCannotPromoteUnprovedCSSCapabilities renderless.TestRequiresEscalation
js.TestPerformanceNowCallbacksRetireNativeHandles
js.TestPerformanceClockOriginUsesCurrentRealmInBothSnapshotModes
js.TestPristineContextReuseRebindsActualPerformanceClock
renderless.TestConformanceCoverageAccountsForEveryRequiredSourceFamily
renderless.TestConformanceCoverageCountsSharedReferencesOnceAndDetachesProfile
js.TestCompatibilityCallbacksCannotSilentlyAcknowledgeFailure
js.TestCompatibilityCallbackFailuresPreserveLaterDelivery
js.TestEmptyCallbackFailuresRemainObservable
js.TestGeolocationDenialCallbacksCannotDiscardFailures
renderless.TestEveryObservedBindingHasOneSourceLinkedClassification
renderless.TestBindingClassificationConflictsCannotPromoteAliasesOrChildren
renderless.TestProductionBindingClassificationsDriveActualRouterDecisions`},
		{"dom", CategorySupportedReal, "window document element Element HTMLElement Node Document querySelector querySelectorAll getElementById getElementsByClassName getElementsByTagName", "DOM-backed tree mutations, selectors and element brands; no layout", `js.TestWindowIsGlobalThis js.TestDocumentTitleAndURL js.TestQuerySelectorTextContent js.TestQuerySelectorAllIndexAccess js.TestJSCreateAppendRemove js.TestJSGetElementsByTagAndClass js.TestHTMLElementInstanceof js.TestMutationObserverRejectedDOMWritesPreserveActualTree webapi.TestInsertionRejectsCyclesBeforeChangingTree webapi.TestInsertBeforeInvalidReferencePreservesSourceAndSelfReference webapi.TestChildInsertionRejectsInvalidOwners
js.TestDOMCallbacksRetireArgumentsAndReturnedHandles
js.TestDOMCookieCallbacksRetireHandlesAndPreserveEffects
js.TestDOMParserAndAttributeCollectionsRetireHandles
js.TestDOMCollectionsIgnoreInheritedSettersAndRetireFailures
js.TestConsoleURLAndComputedStyleCallbacksRetireNativeHandles
js.TestComputedStyleFailuresRetireNativeHandles`},
		{"identity-console", CategorySupportedReal, "navigator console", "Configured navigator identity and bounded string-converting console delivery", `js.TestNavigatorDefaults js.TestNavigatorOverride js.TestConsoleCapture
js.TestConsoleURLAndComputedStyleCallbacksRetireNativeHandles
js.TestConsoleRejectsConversionFailureAndQuotaBeforePublishing`},
		{"events", CategorySupportedReal, "addEventListener removeEventListener dispatchEvent Event CustomEvent", "Owned DOM event propagation, target identity and cancellation", `js.TestAddEventListenerAndDispatch js.TestRemoveEventListener js.TestDocumentWindowAndElementHaveDistinctEventTargets js.TestPreventDefaultRoundTrip js.TestKeyboardAndInputAndFocusAndCustomEvent
js.TestDocumentLoadingPromiseRejectionRemainsStickyAcrossRetry
engine.TestActualHTTPUnhandledPromiseRejectsPageAdmission
js.TestPristineReuseInstallsOnlyCurrentDocumentAttributes
js.TestAttributeCompilationFailureSurvivesPristineReuseUntilLoading
engine.TestActualHTTPAttributeFailureRejectsScriptAdmissionAndPreservesStaticFetch`},
		{"timers", CategorySupportedReal, "setTimeout setInterval clearTimeout clearInterval requestAnimationFrame Promise queueMicrotask", "Bounded scheduled callbacks, microtasks and owned rejection observation; no rendering or frame presentation", `js.TestTimersHonorDueOrderArgumentsIntervalsAndCancellation js.TestTimerCallbacksAndScriptFailuresRemainObservable js.TestTimersBoundAdmissionReleaseCancelledSlotsAndRetireOnClose js.TestQueueMicrotask js.TestRequestAnimationFrame js.TestExecutionCancellationIsOwned
js.TestUnhandledPromiseRejectionsCannotAcknowledgeSuccess
js.TestHandledPromiseRejectionsDoNotFailExecution
js.TestEmptyAndOpaquePromiseReasonsRemainObservableWithoutDispatch
js.TestForeignRealmPromiseRejectionUsesTheActualExecutingOwner
js.TestUnhandledRejectionDoesNotContaminateUntouchedSiblingExecution
js.TestClosingForeignCreationOwnerCannotEraseItsPendingRejection`},
		{"fetch", CategorySupportedReal, "fetch", "Policy-bound request/response adapters, cancellation and actual HTTP integration", `js.TestFetchRequestCopiesExactBinaryViewAndEnforcesPolicy js.TestFetchTransportErrorRejects js.TestSynchronousFetchHonorsEvalCancellation engine.TestEngineFetchEndToEnd
js.TestActualAsyncFetchRejectionSurfacesThroughWaitIdle`},
		{"xhr", CategorySupportedReal, "XMLHttpRequest", "Request adapter state transitions, bodies and observable failure callbacks", "js.TestXHRBasicGET js.TestXHRReadyStateTransitions js.TestXHRPOSTWithBody js.TestXHRErrorFiresOnError js.TestXHRCallbackFailuresRemainObservableWithoutStoppingLaterDelivery"},
		{"headers", CategorySupportedReal, "Headers", "Header validation, ordering, private state and immutable response guards", "js.TestHeadersValidationIterationAndPrivateState js.TestFetchHeadersUseCanonicalInputsAndImmutableResponseGuard"},
		{"request-response", CategorySupportedReal, "Request Response", "Request conversion and retained response/body subset; no full stream contract", "js.TestFetchRequestRejectsConversionFailuresBeforeTransport js.TestFetchResponseBodiesConsumeOnceAndCloneIndependently js.TestFetchResponseBinaryAndJSONAreActualConversions js.TestFetchResponseMetadataAndReceiverCannotBeForged"},
		{"forms", CategorySupportedReal, "FormData HTMLFormElement validityState", "HTML form serialization and tested validity subset", "js.TestFormDataFromForm js.TestFetchWithFormDataBody js.TestFetchFormDataPreservesCallerContentTypeCaseInsensitively js.TestFormValidity"},
		{"url", CategorySupportedReal, "URL URLSearchParams", "URL/base resolution and URLSearchParams value operations", `js.TestURLConstructor js.TestURLSearchParams js.TestFetchURLUsesOwningDocumentAndFirstBase
js.TestConsoleURLAndComputedStyleCallbacksRetireNativeHandles
js.TestURLNativeOutputIgnoresInheritedSettersAndReportsConversionFailures`},
		{"cookies", CategorySupportedReal, "cookie", "Single-owner HTTP/document cookies with HttpOnly read/write separation", `engine.TestDocumentCookieProtectsActualHTTPOnlySessionState js.TestDocumentCookiesRejectOpaqueAndNilCookieAuthority
js.TestClosedPageContextsAreCollectableWhileRuntimeRemainsOpen`},
		{"storage", CategorySupportedReal, "localStorage sessionStorage", "Session/origin/page ownership, quota and callback retirement", `js.TestSessionOriginStorageAndPageIsolation js.TestStorageQuotaFailsAtomicallyAndCanRecover js.TestStorageCallbacksRetireNativeHandles js.TestStorageCallbacksRejectForeignReceiverWithoutNativeAccess
js.TestPristineContextReuseRetiresNativeRebindingHandles`},
		{"history", CategorySupportedReal, "history location", "In-memory history navigation and document location parts", "js.TestHistoryPushReplace js.TestHistoryBackForward js.TestLocationParts"},
		{"crypto", CategorySupportedReal, "SubtleCrypto crypto", "WebCrypto subset: integer-view getRandomValues, randomUUID and SHA-1/256/384/512 digest; key import/generation, encryption, signing, derivation and wrapping are absent and escalate to Chromium", `js.TestCryptoRandomUUID
js.TestCryptoGetRandomValues
js.TestCryptoRandomValuesFillEveryIntegerViewByte
js.TestCryptoRandomValuesRejectInvalidTypesAndByteQuota
js.TestCryptoRandomCallbacksRetireNativeHandles
js.TestCryptoRandomValuesIgnorePagePropertiesAndConstructorReplacement
js.TestCryptoDigestCopiesActualBufferRegions
js.TestCryptoDigestRetiresActualNativeCallbackHandles
js.TestCryptoSubtleDigestSHA256
js.TestCryptoSubtleExposesOnlyDigest`},
		{"custom-elements", CategorySupportedReal, "customElements", "Registration and connected callbacks", "js.TestCustomElementsDefine"},
		{"mutation-observer", CategorySupportedReal, "MutationObserver", "Scoped child/attribute/text records, shared ordered queues, options and bounded ownership", `js.TestMutationObserverChildListAdd
js.TestMutationObserverAttributesChange
js.TestMutationObserverDisconnect
js.TestMutationObserverFailureAndRegistrationOrderAreObservable
js.TestMutationObserverPumpIsBounded
js.TestMutationObserverTakeRecordsPreservesCapturedFields
js.TestMutationObserverTransportIgnoresInheritedSetters
js.TestMutationObserverCallbacksRetireNativeHandles
js.TestMutationObserverRecordConstructionRetiresHandles
js.TestMutationObserverFailedTakePreservesPendingRecords
js.TestMutationObserverDeliveredRecordsRetireNativeHandles
js.TestMutationObserverOptionErrorsDoNotRetainCallbacks
js.TestMutationObserverRegistrationAdmissionIsBoundedAndRecoverable
js.TestMutationObserverPendingCountAdmissionIsAtomicAndRecoverable
js.TestMutationObserverPendingByteAdmissionIsAtomicAndRecoverable
js.TestMutationObserverDeliveryReleasesQuotaBeforeCallback
js.TestMutationObserverPendingOwnershipCountsRetainedBackingAndCloses
js.TestMutationObserverEveryDOMWriterReportsPendingOverflow
js.TestMutationObserverSharesOrderedRecordsAcrossTargets
js.TestMutationObserverTakeRecordsSharesChronologicalQueue
js.TestMutationObserverReobserveReplacesOptionsWithoutDiscardingQueue
js.TestMutationObserverValidatesPublicOptionsBeforeChangingOwnership
js.TestMutationObserverFiltersAndOldValuesArePerObserver
js.TestMutationObserverCharacterDataCapturesActualOldValues
js.TestMutationObserverDeliveryUsesConstructionOrder
js.TestMutationObserverPublicOwnershipSurvivesReobserveAndReconnect
js.TestMutationObserverUsesActualDocumentAndNodeIdentity
js.TestMutationObserverOverlappingTargetsMergeOldValueInterest
js.TestMutationObserverFilterLimitsRejectBeforeChangingRegistration
js.TestMutationObserverOptionsBudgetRecoversAndCloses
js.TestMutationObserverTargetQuotaCountsRegistrationsNotCallbacks
js.TestMutationObserverNativeAdmissionRejectsMalformedMetadataWithoutRetention
js.TestMutationObserverFilterAndRecordsIgnoreInheritedArraySetters
js.TestMutationObserverOldValueBytesParticipateInPendingBudget
js.TestMutationObserverTreeRecordsPreserveActualSiblingPositions
js.TestMutationObserverMovesPreserveRemovalAndAdditionOrder
js.TestMutationObserverRemovedSubtreeSurvivesTakeUntilNotification
js.TestMutationObserverNestedRemovalAndReobserveRetireSourceTracking
js.TestMutationObserverRetiresRemovedSubtreeBeforeItsCallback
js.TestMutationObserverForeignSelfReferenceDoesNotMoveNode
js.TestMutationObserverReplacementRetainsRemovedSubtrees
js.TestMutationObserverTransientCleanupWithoutRecordsIsBounded
js.TestMutationObserverTransientLifetimeIsPerCallbackOwner
js.TestMutationObserverMoveAdmissionDoesNotPartiallyQueue
js.TestMutationObserverReobserveRetiresOnlyMatchingTransientSource
js.TestMutationObserverRepeatedDetachDeduplicatesAndCloseReleases
js.TestMutationObserverTransientLimitsRejectWithoutPartialAdmission
js.TestMutationObserverSameParentAndSelfReferenceProduceRealMoveRecords
js.TestMutationObserverChildWritersRejectForgedNodeArguments
js.TestMutationObserverNativeChildWritersRejectInvalidIdentities
js.TestMutationObserverFailedTreePublicationPreservesTransientOwnership`},
		{"websocket", CategorySupportedReal, "WebSocket", "Owned real socket transport, binary regions, ordered sends, close/protocol handling", "js.TestWebSocketBinaryEchoUsesActualBufferRegion js.TestWebSocketSendsPreserveCallOrder js.TestWebSocketRemoteClosePreservesActualCodeAndReason js.TestWebSocketCloseJoinsActualOpeningWorker js.TestWebSocketListenerFailureUsesTheExistingEventErrorOwner"},
		{"streams", CategorySupportedReal, "ReadableStream WritableStream", "In-memory stream reads, piping, transforms and tee subset with explicit error-state and awaited cleanup results", `js.TestReadableStreamReader js.TestStreamPipeTo js.TestStreamPipeThroughTransform js.TestStreamTee
js.TestStreamCancelPreservesActualCallbackFailureAndClosesPendingReads
js.TestStreamAbortPreservesActualSinkFailureThroughBothEntryPoints
js.TestStreamErrorStatePreservesEveryFalseyReason
js.TestStreamCleanupWaitsForActualCallbackSettlement
js.TestStreamCancelDiscardsQueuedDataAfterCloseWasRequested
js.TestStreamAsyncInitializationAndPullRejectActualConsumers
js.TestStreamFailedWritesAndCloseKeepTheirOriginalError
js.TestStreamStartDefersPullWriteAndCloseUntilActualSettlement
js.TestPublicStreamCloseAwaitsActualSinkAndRunsOnce
js.TestStreamSerializesActualPullsAndServicesEveryWaitingRead
js.TestTransformPipelineFailureReachesTheActualReadableConsumer`},
		{"broadcast", CategorySupportedReal, "BroadcastChannel", "Same-context channel delivery", "js.TestBroadcastChannelDelivers"},
		{"iframe", CategorySupportedReal, "HTMLIFrameElement", "Bounded frame ownership and JSON message delivery with actual HTTP origin checks; no full navigation or structured clone", `js.TestIFrameFailuresCannotAcknowledgePartialSuccess js.TestWindowParentTopSelf js.TestWindowFramesLive
js.TestIFrameCallbacksRetireNativeHandlesAndDeliverActualMessages
js.TestIFrameLoaderFailuresRemainObservableWhenCaught
js.TestIFrameMessageOriginUsesActualRedirectedDocument
js.TestIFrameMessageValidationAndQueueBudgetsPreserveActualOwner
js.TestIFrameAdmissionBoundsRealChildrenAndSourceBytes
js.TestIFrameMessageOriginSerializesCanonicalHTTPOrigins
js.TestActualChildPromiseRejectionRemainsOwnedByParentExecution`},
		{"ranges", CategorySupportedReal, "Range Selection", "DOM-backed range boundaries, text slicing and selection extension", "js.TestRangeCreate js.TestRangeTextSlicing js.TestRangeSetStartBeforeAfter js.TestSelectionExtendAndContains"},
		{"canvas-synthetic", CategoryStubCompatible, "canvas", "Synthetic 2D shape; real pixel semantics require escalation", "js.TestCanvasGetContext js.TestSyntheticCanvasAndWorkersDoNotProvideRealEffects renderless.TestSyntheticConformanceRequiresEscalationForEveryRealSemanticRequest"},
		{"animation-synthetic", CategoryStubCompatible, "Element.animate", "Animation-object compatibility; real animation requires escalation", "js.TestElementAnimateReturnsAnimation renderless.TestSyntheticConformanceRequiresEscalationForEveryRealSemanticRequest"},
		{"layout-observer-synthetic", CategoryStubCompatible, "IntersectionObserver ResizeObserver", "Synthetic one-shot observations; real layout requires escalation", "js.TestIntersectionObserverFires js.TestResizeObserverFires renderless.TestSyntheticConformanceRequiresEscalationForEveryRealSemanticRequest"},
		{"workers-synthetic", CategoryStubCompatible, "Worker SharedWorker", "Constructor/message-method compatibility; no worker execution", "js.TestWorkerStubNoCrash js.TestSyntheticCanvasAndWorkersDoNotProvideRealEffects renderless.TestSyntheticConformanceRequiresEscalationForEveryRealSemanticRequest"},
		{"viewport-synthetic", CategoryStubCompatible, "visualViewport screen matchMedia", "Synthetic viewport/screen/media-query outputs", "js.TestScreenAndVisualViewport js.TestMatchMedia renderless.TestSyntheticConformanceRequiresEscalationForEveryRealSemanticRequest"},
	}
}

func conformanceNames(category CapabilityCategory) []string {
	var names []string
	for _, definition := range conformanceDefinitions() {
		if definition.category == category {
			names = append(names, strings.Fields(definition.names)...)
		}
	}
	return names
}

func (c *compiledConformance) evidenceFor(ids map[string]bool) []WebAPIConformanceEvidence {
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	result := make([]WebAPIConformanceEvidence, 0, len(keys))
	for _, id := range keys {
		evidence := c.cases[id]
		evidence.Tests = append([]ConformanceTestReference{}, evidence.Tests...)
		result = append(result, evidence)
	}
	return result
}
