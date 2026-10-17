package v8go_test

import (
	"bytes"
	"testing"

	v8 "github.com/tommie/v8go"
)

func TestArrayBufferCopiesExactOwnedBytes(t *testing.T) {
	isolate := v8.NewIsolate()
	defer isolate.Dispose()
	ctx := v8.NewContext(isolate)
	defer ctx.Close()
	input := []byte{0, 255, 128, 1}
	value, err := ctx.NewArrayBuffer(input)
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	input[0] = 42
	actual, err := value.BufferBytes(4)
	if err != nil || !bytes.Equal(actual, []byte{0, 255, 128, 1}) {
		t.Fatalf("binary copy changed bytes or borrowed Go memory: %v %v", actual, err)
	}
	actual[1] = 9
	again, err := value.BufferBytes(4)
	if err != nil || again[1] != 255 {
		t.Fatalf("returned bytes borrowed V8 backing memory: %v %v", again, err)
	}
	if data, err := value.BufferBytes(3); data != nil || err == nil {
		t.Fatalf("byte bound did not fail closed: %v %v", data, err)
	}
}

func TestArrayBufferViewRegionsAndRejections(t *testing.T) {
	isolate := v8.NewIsolate()
	defer isolate.Dispose()
	ctx := v8.NewContext(isolate)
	defer ctx.Close()
	for _, test := range []struct {
		name, source string
		want         []byte
		invalid      bool
	}{
		{"typed view", `new Uint8Array(new Uint8Array([7,0,255,9]).buffer,1,2)`, []byte{0, 255}, false},
		{"data view", `new DataView(new Uint8Array([7,0,255,9]).buffer,2,1)`, []byte{255}, false},
		{"empty", `new ArrayBuffer(0)`, []byte{}, false},
		{"not bytes", `({byteLength:2})`, nil, true},
		{"shared", `new SharedArrayBuffer(2)`, nil, true},
		{"shared view", `new Uint8Array(new SharedArrayBuffer(2))`, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := ctx.RunScript(test.source, "<native-buffer-control>")
			if err != nil {
				t.Fatal(err)
			}
			defer value.Release()
			actual, err := value.BufferBytes(8)
			if test.invalid {
				if err == nil || actual != nil {
					t.Fatalf("invalid source gained bytes: %v %v", actual, err)
				}
				return
			}
			if err != nil || !bytes.Equal(actual, test.want) {
				t.Fatalf("wrong view region: %v %v", actual, err)
			}
		})
	}
}

func TestArrayBufferWritesExactViewAndRejectsPartialWrites(t *testing.T) {
	isolate := v8.NewIsolate()
	defer isolate.Dispose()
	ctx := v8.NewContext(isolate)
	defer ctx.Close()
	value, err := ctx.RunScript(`globalThis.backing=new Uint8Array([7,8,9,10,11,12]);new Uint16Array(backing.buffer,2,1)`, "<native-buffer-write>")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	if length, err := value.BufferByteLength(); err != nil || length != 2 {
		t.Fatalf("view length=%d err=%v", length, err)
	}
	for _, input := range [][]byte{{1}, {1, 2, 3}} {
		if err := value.SetBufferBytes(input); err == nil {
			t.Fatal("inexact write accepted")
		}
	}
	unchanged, err := value.BufferBytes(2)
	if err != nil || !bytes.Equal(unchanged, []byte{9, 10}) {
		t.Fatalf("rejected write changed region: %v %v", unchanged, err)
	}
	input := []byte{0, 255}
	if err := value.SetBufferBytes(input); err != nil {
		t.Fatal(err)
	}
	input[0] = 99
	actual, err := ctx.RunScript(`Array.from(backing).join(',')`, "<native-write-result>")
	if err != nil {
		t.Fatal(err)
	}
	defer actual.Release()
	if actual.String() != "7,8,0,255,11,12" {
		t.Fatalf("write escaped view or borrowed bytes: %s", actual.String())
	}
	for _, source := range []string{`({byteLength:0})`, `new SharedArrayBuffer(0)`, `new Uint8Array(new SharedArrayBuffer(0))`} {
		invalid, err := ctx.RunScript(source, "<invalid-native-write>")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invalid.BufferByteLength(); err == nil {
			invalid.Release()
			t.Fatal("invalid region length accepted")
		}
		if err := invalid.SetBufferBytes(nil); err == nil {
			invalid.Release()
			t.Fatal("invalid region write accepted")
		}
		invalid.Release()
	}
	empty, err := ctx.NewArrayBuffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Release()
	if err := empty.SetBufferBytes(nil); err != nil {
		t.Fatal(err)
	}
}
