package js

import "testing"

func TestStreamAsyncInitializationAndPullRejectActualConsumers(t *testing.T) {
	for _, operation := range []string{
		`new ReadableStream({start(){return Promise.reject(reason)}}).getReader().read()`,
		`new ReadableStream({pull(){return Promise.reject(reason)}}).getReader().read()`,
		`new WritableStream({start(){return Promise.reject(reason)}}).getWriter().write(1)`,
		`new WritableStream({start(){return Promise.reject(reason)}}).close()`,
		`new TransformStream({start(){return Promise.reject(reason)}}).readable.getReader().read()`,
		`new TransformStream({start(){return Promise.reject(reason)}}).writable.getWriter().write(1)`,
	} {
		t.Run(operation, func(t *testing.T) {
			c := newCtxFromHTML(t, `<body></body>`, nil)
			if err := c.EvalDiscard(t.Context(), `globalThis.result='pending';const reason={actual:true};`+operation+`.then(()=>{result='false success'},value=>{result=value===reason?'exact failure':'wrong failure'})`); err != nil {
				t.Fatalf("consumer could not own its handled rejection: %v", err)
			}
			if got, err := c.EvalString(t.Context(), `result`); err != nil || got != "exact failure" {
				t.Fatalf("async callback failure did not reach actual consumer: %q %v", got, err)
			}
		})
	}
}

func TestStreamFailedWritesAndCloseKeepTheirOriginalError(t *testing.T) {
	for _, entry := range []string{"write", "close"} {
		for _, failure := range []string{`throw reason`, `return Promise.reject(reason)`} {
			t.Run(entry+failure, func(t *testing.T) {
				c := newCtxFromHTML(t, `<body></body>`, nil)
				if err := c.EvalDiscard(t.Context(), `globalThis.result='pending';let calls=0;const reason={actual:true};
const writer=new WritableStream({`+entry+`(){calls++;`+failure+`}}).getWriter();
writer.`+entry+`('actual').then(()=>{result='false success'},async value=>{
if(value!==reason)throw Error('first reason changed');
for(const operation of [()=>writer.write(2),()=>writer.close()]){
try{await operation();throw Error('later operation succeeded')}catch(value){if(value!==reason)throw value}
}result=calls===1?'exact failure':'duplicate callback';});`); err != nil {
					t.Fatal(err)
				}
				if got, err := c.EvalString(t.Context(), `result`); err != nil || got != "exact failure" {
					t.Fatalf("stream lost terminal failure state: %q %v", got, err)
				}
			})
		}
	}
}

func TestStreamStartDefersPullWriteAndCloseUntilActualSettlement(t *testing.T) {
	for _, entry := range []string{"pull", "write", "close", "abort"} {
		t.Run(entry, func(t *testing.T) {
			c := newCtxFromHTML(t, `<body></body>`, nil)
			source := `globalThis.result='pending';globalThis.calls=0;globalThis.releaseStart=null;const start=()=>new Promise(resolve=>releaseStart=resolve);`
			if entry == "pull" {
				source += `new ReadableStream({start,pull(c){calls++;c.enqueue('actual')}}).getReader().read().then(value=>result=value.value);`
			} else {
				source += `new WritableStream({start,` + entry + `(){calls++}}).getWriter().` + entry + `().then(()=>result='actual');`
			}
			if err := c.EvalDiscard(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), `result+':'+calls`); err != nil || got != "pending:0" {
				t.Fatalf("work started before source initialization settled: %q %v", got, err)
			}
			if err := c.EvalDiscard(t.Context(), `releaseStart()`); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), `result+':'+calls`); err != nil || got != "actual:1" {
				t.Fatalf("initialized source did not service actual operation: %q %v", got, err)
			}
		})
	}
}

func TestPublicStreamCloseAwaitsActualSinkAndRunsOnce(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	if err := c.EvalDiscard(t.Context(), `
globalThis.result='pending';globalThis.calls=0;globalThis.releaseClose=null;
const stream=new WritableStream({close(){calls++;return new Promise(resolve=>releaseClose=resolve)}});
stream.close().then(value=>result=value===undefined?'actual':'wrong value');
`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), `result+':'+calls`); err != nil || got != "pending:1" {
		t.Fatalf("public close acknowledged before sink cleanup: %q %v", got, err)
	}
	if err := c.EvalDiscard(t.Context(), `
globalThis.refused=0;
const rejectInvalid=value=>{if(!(value instanceof TypeError))throw value;refused++};
const unexpected=()=>{throw Error('invalid operation acknowledged')};
stream.close().then(unexpected,rejectInvalid);
const writer=stream.getWriter();writer.write('after close').then(unexpected,rejectInvalid);
stream.close().then(unexpected,rejectInvalid);writer.releaseLock();`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), `String(refused)+':'+calls`); err != nil || got != "3:1" {
		t.Fatalf("pending close admitted repeated cleanup or new work: %q %v", got, err)
	}
	if err := c.EvalDiscard(t.Context(), `releaseClose('discard returned value')`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), `result+':'+calls`); err != nil || got != "actual:1" {
		t.Fatalf("public close did not settle actual cleanup: %q %v", got, err)
	}
	if err := c.EvalDiscard(t.Context(), `stream.close().then(unexpected,rejectInvalid)`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), `String(refused)+':'+calls`); err != nil || got != "4:1" {
		t.Fatalf("completed close admitted repeated cleanup: %q %v", got, err)
	}
}

func TestStreamSerializesActualPullsAndServicesEveryWaitingRead(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	if err := c.EvalDiscard(t.Context(), `
globalThis.values=[];globalThis.calls=0;globalThis.active=0;globalThis.peak=0;globalThis.releasePull=null;
const reader=new ReadableStream({pull(controller){
calls++;active++;peak=Math.max(peak,active);const value=calls;
return new Promise(resolve=>releasePull=()=>{controller.enqueue(value);active--;resolve()});
}}).getReader();
for(let index=0;index<3;index++)reader.read().then(value=>values.push(value.value));
`); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1:1:1:", "2:1:1:1", "3:1:1:1,2", "3:0:1:1,2,3"} {
		if got, err := c.EvalString(t.Context(), `calls+':'+active+':'+peak+':'+values.join(',')`); err != nil || got != want {
			t.Fatalf("source pull overlapped or stranded a waiting reader: got=%q want=%q error=%v", got, want, err)
		}
		if want != "3:0:1:1,2,3" {
			if err := c.EvalDiscard(t.Context(), `releasePull()`); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestTransformPipelineFailureReachesTheActualReadableConsumer(t *testing.T) {
	for _, callback := range []string{"transform", "flush"} {
		for _, failure := range []string{`throw reason`, `return Promise.reject(reason)`} {
			t.Run(callback+failure, func(t *testing.T) {
				c := newCtxFromHTML(t, `<body></body>`, nil)
				if err := c.EvalDiscard(t.Context(), `globalThis.result='pending';const reason={actual:true};
const source=new ReadableStream({start(c){`+map[string]string{"transform": "c.enqueue(1);", "flush": ""}[callback]+`c.close()}});
source.pipeThrough(new TransformStream({`+callback+`(){`+failure+`}})).getReader().read().then(()=>{result='false success'},value=>{result=value===reason?'exact failure':'wrong failure'});`); err != nil {
					t.Fatal(err)
				}
				if got, err := c.EvalString(t.Context(), `result`); err != nil || got != "exact failure" {
					t.Fatalf("pipeline hid its actual transform failure: %q %v", got, err)
				}
			})
		}
	}
}
