// Artemis additions to v8go. See ARTEMIS_PATCHES.md.
#ifndef V8GO_ARTEMIS_H
#define V8GO_ARTEMIS_H

#include <stdint.h>

#include "errors.h"
#include "isolate.h"
#include "value.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef struct m_snapshotCreator m_snapshotCreator;
typedef m_snapshotCreator* SnapshotCreatorPtr;

typedef struct {
  const uint8_t* data;
  int length;
  RtnError error;
} SnapshotBlob;

extern SnapshotCreatorPtr SnapshotCreatorNew();
extern RtnError SnapshotCreatorRunScript(SnapshotCreatorPtr sc,
                                         const char* source,
                                         const char* origin);
extern SnapshotBlob SnapshotCreatorCreateBlob(SnapshotCreatorPtr sc);
extern void SnapshotCreatorDelete(SnapshotCreatorPtr sc);
extern void SnapshotBlobFree(uint8_t* data);

typedef struct {
  uint64_t length;
  RtnError error;
} RtnBufferSize;

extern RtnValue ContextNewArrayBuffer(ContextPtr ctx,
                                      const uint8_t* data,
                                      int length);
extern RtnValue ContextNewNullPrototypeObject(ContextPtr ctx);
extern RtnValue ContextNewTypeError(ContextPtr ctx,
                                    const char* message,
                                    int length);
extern RtnBufferSize ValueBufferSize(ValuePtr ptr, int max_bytes);
extern RtnBufferSize ValueBufferByteLength(ValuePtr ptr);
extern RtnError ValueCopyBuffer(ValuePtr ptr, uint8_t* destination, int length);
extern RtnError ValueWriteBuffer(ValuePtr ptr, const uint8_t* source, int length);

#define V8GO_MAX_PROMISE_REJECTIONS 4096
extern RtnError ContextEnablePromiseRejectionTracking(ContextPtr ctx);
extern RtnString ContextTakePromiseRejections(ContextPtr ctx);

extern void ValueReleaseAfterCallback(ValuePtr ptr);
extern RtnString ValueToStringChecked(ValuePtr ptr);
extern RtnString ValueToStringCheckedBounded(ValuePtr ptr, int max_bytes);
extern RtnError ValueDeleteMapKey(ValuePtr ptr, ValuePtr key);

#ifdef __cplusplus
}  // extern "C"
#endif
#endif
