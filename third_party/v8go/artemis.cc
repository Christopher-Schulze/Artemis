//go:build !darwin

// On darwin the binding is linked from the prebuilt lib/<os_arch> archive
// (see ARTEMIS_PATCHES.md); elsewhere cgo compiles this file.

// Artemis additions to v8go. See ARTEMIS_PATCHES.md.
#include "artemis.h"

#include <stdlib.h>
#include <string.h>

#include <string>
#include <vector>

#include "_cgo_export.h"
#include "context-macros.h"
#include "context.h"
#include "deps/include/v8-array-buffer.h"
#include "deps/include/v8-container.h"
#include "deps/include/v8-exception.h"
#include "deps/include/v8-microtask-queue.h"
#include "deps/include/v8-promise.h"
#include "deps/include/v8-script.h"
#include "deps/include/v8-snapshot.h"
#include "isolate-macros.h"
#include "utils.h"
#include "value-macros.h"

using namespace v8;

extern ArrayBuffer::Allocator* default_allocator;

static RtnError ErrorWithMessage(const char* message) {
  RtnError rtn = {};
  rtn.msg = CopyString(message);
  return rtn;
}

static m_value* NewTrackedValue(m_ctx* ctx, Isolate* iso, Local<Value> value) {
  m_value* val = new m_value;
  val->id = 0;
  val->iso = iso;
  val->ctx = ctx;
  val->ptr = Global<Value>(iso, value);
  return tracked_value(ctx, val);
}

/********** Snapshot creator **********/

// Lifecycle: New -> RunScript* -> CreateBlob -> Delete. The creator owns its
// isolate; CreateBlob serialises the default context and the produced bytes
// are heap-copied so they outlive the creator.
struct m_snapshotCreator {
  SnapshotCreator* creator;
  Isolate* iso;
  Global<Context> ctx;
  bool finalized;
};

SnapshotCreatorPtr SnapshotCreatorNew() {
  m_snapshotCreator* sc = new m_snapshotCreator;
  Isolate::CreateParams params;
  params.array_buffer_allocator = default_allocator;
  sc->creator = new SnapshotCreator(params);
  sc->iso = sc->creator->GetIsolate();
  sc->finalized = false;
  HandleScope handle_scope(sc->iso);
  sc->ctx.Reset(sc->iso, Context::New(sc->iso));
  return sc;
}

RtnError SnapshotCreatorRunScript(SnapshotCreatorPtr sc,
                                  const char* source,
                                  const char* origin) {
  if (sc == nullptr || sc->finalized) {
    return ErrorWithMessage("snapshot creator unavailable");
  }
  HandleScope handle_scope(sc->iso);
  Local<Context> ctx = sc->ctx.Get(sc->iso);
  Context::Scope context_scope(ctx);
  TryCatch try_catch(sc->iso);
  Local<String> src;
  Local<String> ori;
  if (!String::NewFromUtf8(sc->iso, source).ToLocal(&src)) {
    return ErrorWithMessage("invalid source string");
  }
  if (!String::NewFromUtf8(sc->iso, origin).ToLocal(&ori)) {
    return ErrorWithMessage("invalid origin string");
  }
  ScriptOrigin script_origin(ori);
  Local<Script> script;
  if (!Script::Compile(ctx, src, &script_origin).ToLocal(&script)) {
    return ExceptionError(try_catch, sc->iso, ctx);
  }
  Local<Value> result;
  if (!script->Run(ctx).ToLocal(&result)) {
    return ExceptionError(try_catch, sc->iso, ctx);
  }
  return {};
}

SnapshotBlob SnapshotCreatorCreateBlob(SnapshotCreatorPtr sc) {
  SnapshotBlob result = {};
  if (sc == nullptr || sc->finalized) {
    result.error = ErrorWithMessage("snapshot creator unavailable");
    return result;
  }
  {
    // The default context stays pristine so plain contexts of isolates
    // using this process snapshot are unchanged; the scripted realm is
    // additional context 0 (NewContextFromSnapshot).
    HandleScope handle_scope(sc->iso);
    sc->creator->SetDefaultContext(Context::New(sc->iso));
    size_t index = sc->creator->AddContext(sc->ctx.Get(sc->iso));
    sc->ctx.Reset();
    if (index != 0) {
      result.error = ErrorWithMessage("snapshot creator: unexpected context index");
      return result;
    }
  }
  StartupData blob =
      sc->creator->CreateBlob(SnapshotCreator::FunctionCodeHandling::kClear);
  sc->finalized = true;
  if (blob.data == nullptr || blob.raw_size <= 0) {
    delete[] blob.data;
    result.error = ErrorWithMessage("CreateBlob returned empty snapshot");
    return result;
  }
  uint8_t* copy = static_cast<uint8_t*>(malloc(blob.raw_size));
  memcpy(copy, blob.data, blob.raw_size);
  delete[] blob.data;
  result.data = copy;
  result.length = blob.raw_size;
  return result;
}

void SnapshotCreatorDelete(SnapshotCreatorPtr sc) {
  if (sc == nullptr) {
    return;
  }
  if (!sc->ctx.IsEmpty()) {
    HandleScope handle_scope(sc->iso);
    sc->ctx.Reset();
  }
  delete sc->creator;
  delete sc;
}

void SnapshotBlobFree(uint8_t* data) {
  free(data);
}

/********** Owned values **********/

RtnValue ContextNewNullPrototypeObject(ContextPtr ctx) {
  LOCAL_CONTEXT(ctx);
  RtnValue rtn = {};
  Local<Object> object = Object::New(iso, Null(iso), nullptr, nullptr, 0);
  rtn.value = NewTrackedValue(ctx, iso, object);
  return rtn;
}

RtnValue ContextNewArrayBuffer(ContextPtr ctx, const uint8_t* data, int length) {
  LOCAL_CONTEXT(ctx);
  RtnValue rtn = {};
  if (length < 0 || (length > 0 && data == nullptr)) {
    rtn.error = ErrorWithMessage("ArrayBuffer: invalid input bytes");
    return rtn;
  }
  Local<ArrayBuffer> buffer;
  if (!ArrayBuffer::MaybeNew(iso, static_cast<size_t>(length))
           .ToLocal(&buffer)) {
    rtn.error = ErrorWithMessage("ArrayBuffer: allocation failed");
    return rtn;
  }
  if (length > 0) {
    memcpy(buffer->Data(), data, static_cast<size_t>(length));
  }
  rtn.value = NewTrackedValue(ctx, iso, buffer);
  return rtn;
}

RtnValue ContextNewTypeError(ContextPtr ctx, const char* message, int length) {
  LOCAL_CONTEXT(ctx);
  RtnValue rtn = {};
  Local<String> text;
  if (!String::NewFromUtf8(iso, message, NewStringType::kNormal, length)
           .ToLocal(&text)) {
    rtn.error = ExceptionError(try_catch, iso, local_ctx);
    return rtn;
  }
  Local<Value> error = Exception::TypeError(text);
  if (error.IsEmpty()) {
    rtn.error = ExceptionError(try_catch, iso, local_ctx);
    return rtn;
  }
  rtn.value = NewTrackedValue(ctx, iso, error);
  return rtn;
}

void ValueReleaseAfterCallback(ValuePtr ptr) {
  ptr->release_after_callback = true;
}

RtnString ValueToStringChecked(ValuePtr ptr) {
  return ValueToStringCheckedBounded(ptr, -1);
}

RtnString ValueToStringCheckedBounded(ValuePtr ptr, int max_bytes) {
  LOCAL_VALUE(ptr);
  RtnString rtn = {};
  Local<String> text;
  if (!value->ToString(local_ctx).ToLocal(&text)) {
    rtn.error = ExceptionError(try_catch, iso, local_ctx);
    return rtn;
  }
  if (max_bytes >= 0 &&
      (text->Length() > static_cast<uint32_t>(max_bytes) ||
       text->Utf8Length(iso) > static_cast<size_t>(max_bytes))) {
    rtn.error = ErrorWithMessage("v8go: string byte limit exceeded");
    return rtn;
  }
  size_t length = text->Utf8Length(iso);
  rtn.data = static_cast<char*>(malloc(length + 1));
  text->WriteUtf8(iso, rtn.data, length, String::WriteFlags::kReplaceInvalidUtf8);
  rtn.data[length] = 0;
  rtn.length = static_cast<int>(length);
  return rtn;
}

RtnError ValueDeleteMapKey(ValuePtr ptr, ValuePtr key) {
  LOCAL_VALUE(ptr);
  MicrotasksScope microtasks(local_ctx, MicrotasksScope::kDoNotRunMicrotasks);
  if (!value->IsMap() || key == nullptr || key->iso != iso) {
    return ErrorWithMessage(
        "Map deletion requires an actual Map and same-isolate key");
  }
  if (value.As<Map>()->Delete(local_ctx, key->ptr.Get(iso)).IsNothing()) {
    return ExceptionError(try_catch, iso, local_ctx);
  }
  return {};
}

/********** Bounded binary transfer **********/

static RtnError BufferRegion(Local<Value> value, void** data, size_t* length) {
  Local<ArrayBuffer> buffer;
  size_t offset = 0;
  if (value->IsSharedArrayBuffer()) {
    return ErrorWithMessage("Shared or detached ArrayBuffer is not admitted");
  }
  if (value->IsArrayBuffer()) {
    buffer = value.As<ArrayBuffer>();
    *length = buffer->ByteLength();
  } else if (value->IsArrayBufferView()) {
    Local<ArrayBufferView> view = value.As<ArrayBufferView>();
    if (view->Buffer().IsEmpty()) {
      return ErrorWithMessage("ArrayBuffer view has no backing buffer");
    }
    buffer = view->Buffer();
    offset = view->ByteOffset();
    *length = view->ByteLength();
  } else {
    return ErrorWithMessage("ArrayBuffer or view required");
  }
  if (buffer->WasDetached() || buffer->GetBackingStore()->IsShared()) {
    return ErrorWithMessage("Shared or detached ArrayBuffer is not admitted");
  }
  if (offset > buffer->ByteLength() ||
      *length > buffer->ByteLength() - offset) {
    return ErrorWithMessage("ArrayBuffer view exceeds backing bytes");
  }
  *data = *length == 0 ? nullptr
                       : static_cast<uint8_t*>(buffer->Data()) + offset;
  return {};
}

RtnBufferSize ValueBufferSize(ValuePtr ptr, int max_bytes) {
  LOCAL_VALUE(ptr);
  RtnBufferSize rtn = {};
  void* data = nullptr;
  size_t length = 0;
  rtn.error = BufferRegion(value, &data, &length);
  if (rtn.error.msg != nullptr) {
    return rtn;
  }
  if (max_bytes < 0 || length > static_cast<size_t>(max_bytes)) {
    rtn.error = ErrorWithMessage("ArrayBuffer exceeds byte limit");
    return rtn;
  }
  rtn.length = static_cast<uint64_t>(length);
  return rtn;
}

RtnBufferSize ValueBufferByteLength(ValuePtr ptr) {
  LOCAL_VALUE(ptr);
  RtnBufferSize rtn = {};
  void* data = nullptr;
  size_t length = 0;
  rtn.error = BufferRegion(value, &data, &length);
  if (rtn.error.msg == nullptr) {
    rtn.length = static_cast<uint64_t>(length);
  }
  return rtn;
}

RtnError ValueCopyBuffer(ValuePtr ptr, uint8_t* destination, int length) {
  LOCAL_VALUE(ptr);
  void* data = nullptr;
  size_t actual_length = 0;
  RtnError error = BufferRegion(value, &data, &actual_length);
  if (error.msg != nullptr) {
    return error;
  }
  if (length < 0 || actual_length != static_cast<size_t>(length) ||
      (length > 0 && destination == nullptr)) {
    return ErrorWithMessage("ArrayBuffer changed before byte copy");
  }
  if (length > 0) {
    memcpy(destination, data, actual_length);
  }
  return {};
}

RtnError ValueWriteBuffer(ValuePtr ptr, const uint8_t* source, int length) {
  LOCAL_VALUE(ptr);
  void* data = nullptr;
  size_t actual_length = 0;
  RtnError error = BufferRegion(value, &data, &actual_length);
  if (error.msg != nullptr) {
    return error;
  }
  if (length < 0 || actual_length != static_cast<size_t>(length) ||
      (length > 0 && source == nullptr)) {
    return ErrorWithMessage("ArrayBuffer write requires exact region bytes");
  }
  if (length > 0) {
    memcpy(data, source, actual_length);
  }
  return {};
}

/********** Promise rejection tracking **********/

struct m_promise_rejection {
  int identity_hash;
  Global<Promise> promise;
  Global<Value> reason;
  m_promise_rejection(Isolate* iso,
                      Local<Promise> input,
                      Local<Value> value,
                      int hash)
      : identity_hash(hash), promise(iso, input), reason(iso, value) {}
};

static bool ContextReference(Local<Context> context, int* reference) {
  if (context->GetNumberOfEmbedderDataFields() <= ContextDataIndex::REF) {
    return false;
  }
  Local<Data> data = context->GetEmbedderDataV2(ContextDataIndex::REF);
  if (data.IsEmpty() || !data->IsValue()) {
    return false;
  }
  Local<Value> value = data.As<Value>();
  if (!value->IsInt32()) {
    return false;
  }
  *reference = value.As<Int32>()->Value();
  return true;
}

static void TrackPromiseRejection(PromiseRejectMessage message) {
  Local<Promise> promise = message.GetPromise();
  Isolate* iso = Isolate::GetCurrent();
  Local<Context> creation;
  if (!promise->GetCreationContext(iso).ToLocal(&creation)) {
    return;
  }
  int reference = 0;
  if (!ContextReference(creation, &reference)) {
    return;
  }
  m_ctx* internal = static_cast<m_ctx*>(iso->GetData(0));
  if (internal == nullptr) {
    return;
  }
  auto found = internal->rejection_contexts.find(reference);
  if (found == internal->rejection_contexts.end()) {
    return;
  }
  m_ctx* owner = found->second;
  if (owner->ptr.Get(iso) != creation || owner->rejections == nullptr) {
    return;
  }
  int identity_hash = promise->GetIdentityHash();
  for (auto item = owner->rejections->begin(); item != owner->rejections->end();
       ++item) {
    if (item->identity_hash != identity_hash ||
        item->promise.Get(iso) != promise) {
      continue;
    }
    if (message.GetEvent() == kPromiseHandlerAddedAfterReject) {
      owner->rejections->erase(item);
    }
    return;
  }
  if (message.GetEvent() != kPromiseRejectWithNoHandler) {
    return;
  }
  goPromiseRejection(reference);
  if (owner->rejections->size() >= V8GO_MAX_PROMISE_REJECTIONS) {
    owner->rejection_overflow = true;
    return;
  }
  owner->rejections->emplace_back(iso, promise, message.GetValue(),
                                  identity_hash);
}

RtnError ContextEnablePromiseRejectionTracking(ContextPtr ctx) {
  LOCAL_CONTEXT(ctx);
  int reference = 0;
  if (!ContextReference(local_ctx, &reference)) {
    return ErrorWithMessage("Promise tracking requires an owned context");
  }
  m_ctx* internal = static_cast<m_ctx*>(iso->GetData(0));
  if (ctx->rejections == nullptr) {
    ctx->rejections = new std::vector<m_promise_rejection>();
  }
  internal->rejection_contexts[reference] = ctx;
  iso->SetPromiseRejectCallback(TrackPromiseRejection);
  return {};
}

static std::string RejectionText(Isolate* iso,
                                 Local<Context> context,
                                 Local<Value> reason) {
  if (reason->IsNativeError()) {
    Local<Value> descriptor;
    Local<String> key = String::NewFromUtf8Literal(iso, "message");
    if (reason.As<Object>()
            ->GetOwnPropertyDescriptor(context, key)
            .ToLocal(&descriptor) &&
        descriptor->IsObject()) {
      Local<Object> fields = descriptor.As<Object>();
      Local<String> value_key = String::NewFromUtf8Literal(iso, "value");
      Local<Value> value;
      if (fields->HasOwnProperty(context, value_key).FromMaybe(false) &&
          fields->Get(context, value_key).ToLocal(&value) && value->IsString()) {
        reason = value;
      }
    }
  }
  if (reason->IsString()) {
    Local<String> text = reason.As<String>();
    if (text->Length() > 2048) {
      return "[string reason exceeds diagnostic limit]";
    }
    char buffer[2048];
    size_t characters = 0;
    size_t length =
        text->WriteUtf8(iso, buffer, sizeof(buffer),
                        String::WriteFlags::kReplaceInvalidUtf8, &characters);
    std::string result(buffer, length);
    if (characters < text->Length()) {
      result += "...";
    }
    return result;
  }
  if (reason->IsUndefined()) return "undefined";
  if (reason->IsNull()) return "null";
  if (reason->IsBoolean()) {
    return reason.As<Boolean>()->Value() ? "true" : "false";
  }
  if (reason->IsNumber()) {
    return std::to_string(reason.As<Number>()->Value());
  }
  return "[non-string rejection reason]";
}

RtnString ContextTakePromiseRejections(ContextPtr ctx) {
  LOCAL_CONTEXT(ctx);
  RtnString result = {};
  if ((ctx->rejections == nullptr || ctx->rejections->empty()) &&
      !ctx->rejection_overflow) {
    return result;
  }
  std::string diagnostics;
  if (!iso->IsExecutionTerminating() && ctx->rejections != nullptr) {
    size_t reported = 0;
    for (auto& item : *ctx->rejections) {
      if (reported++ == 16) break;
      if (!diagnostics.empty()) diagnostics += "\n";
      diagnostics += "reason: ";
      diagnostics += RejectionText(iso, local_ctx, item.reason.Get(iso));
    }
    if (ctx->rejections->size() > 16) {
      diagnostics += "\nAdditional unhandled rejections omitted";
    }
    if (ctx->rejection_overflow) {
      diagnostics += "\nPromise rejection tracking limit exceeded";
    }
  }
  if (ctx->rejections != nullptr) {
    ctx->rejections->clear();
  }
  ctx->rejection_overflow = false;
  result.data = CopyString(diagnostics);
  result.length = static_cast<int>(diagnostics.size());
  return result;
}

void ArtemisContextRetireRejections(m_ctx* ctx) {
  Isolate* iso = ctx->iso;
  if (iso == nullptr) {
    return;
  }
  m_ctx* internal = static_cast<m_ctx*>(iso->GetData(0));
  if (internal == ctx) {
    if (!internal->rejection_contexts.empty()) {
      Locker locker(iso);
      iso->SetPromiseRejectCallback(nullptr);
    }
    internal->rejection_contexts.clear();
  } else if (internal != nullptr) {
    for (auto item = internal->rejection_contexts.begin();
         item != internal->rejection_contexts.end(); ++item) {
      if (item->second == ctx) {
        internal->rejection_contexts.erase(item);
        break;
      }
    }
  }
  if (ctx->rejections != nullptr) {
    Locker locker(iso);
    Isolate::Scope isolate_scope(iso);
    ctx->rejections->clear();
    delete ctx->rejections;
    ctx->rejections = nullptr;
  }
}
