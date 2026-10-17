package js

import "testing"

func TestCryptoRandomValuesFillEveryIntegerViewByte(t *testing.T) {
	c := newCtxFromHTML(t, `<div></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `(() => {
		for (const Constructor of [Int8Array, Uint8Array, Uint8ClampedArray, Int16Array, Uint16Array, Int32Array, Uint32Array, BigInt64Array, BigUint64Array]) {
			const backing = new Uint8Array(1024); backing.fill(71);
			const array = new Constructor(backing.buffer, 16, 64);
			if (crypto.getRandomValues(array) !== array) throw Error('random fill lost identity');
			const end = 16 + array.byteLength;
			if (backing.slice(0,16).some(value => value !== 71) || backing.slice(end).some(value => value !== 71)) throw Error('random fill escaped view');
			for(let byte=0; byte<Constructor.BYTES_PER_ELEMENT; byte++) {
				let changed=false;
				for(let index=0;index<64;index++) if(backing[16 + index*Constructor.BYTES_PER_ELEMENT + byte] !== 71) changed=true;
				if(!changed) throw Error(Constructor.name + ' left an entire byte lane unchanged');
			}
			const empty = new Constructor(0);
			if (crypto.getRandomValues(empty) !== empty) throw Error('empty view lost identity');
		}
	})()`); err != nil {
		t.Fatal(err)
	}
}

func TestCryptoRandomValuesRejectInvalidTypesAndByteQuota(t *testing.T) {
	c := newCtxFromHTML(t, `<div></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `(() => {
		for(const value of [[], {}, new Float32Array(2), new Float64Array(2), new DataView(new ArrayBuffer(8)), new ArrayBuffer(8), new Proxy(new Uint8Array(2),{})]) {
			let failure;
			try { crypto.getRandomValues(value); } catch(error) { failure=error; }
			if (!(failure instanceof DOMException) || failure.name !== 'TypeMismatchError' || failure.code !== 17) throw Error('invalid random target accepted or error lost');
		}
		for(const value of [new Uint8Array(65537),new Uint16Array(32769),new BigUint64Array(8193)]) {
			let failure;
			try { crypto.getRandomValues(value); } catch(error) { failure=error; }
			if (!(failure instanceof DOMException) || failure.name !== 'QuotaExceededError') throw Error('random quota must measure bytes');
			if(new Uint8Array(value.buffer).some(byte => byte !== 0)) throw Error('rejected fill changed bytes');
		}
		if(crypto.getRandomValues(new Uint8Array(65536)).byteLength!==65536) throw Error('exact byte limit rejected');
	})()`); err != nil {
		t.Fatal(err)
	}
}

func TestCryptoRandomCallbacksRetireNativeHandles(t *testing.T) {
	c := newCtxFromHTML(t, `<div></div>`, nil)
	const script = `for(let index=0;index<100;index++){const array=new Uint32Array(4); if(crypto.getRandomValues(array)!==array)throw Error('identity lost'); if(!/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(crypto.randomUUID()))throw Error('UUID format lost');}`
	if err := c.EvalDiscard(t.Context(), script); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), script); err != nil {
			t.Fatal(err)
		}
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("random callbacks retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestCryptoRandomValuesIgnorePagePropertiesAndConstructorReplacement(t *testing.T) {
	c := newCtxFromHTML(t, `<div></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `(() => {
		const backing=new Uint8Array(256);
		const array=new Uint32Array(backing.buffer,16,32);
		for(const name of ['length','byteLength','byteOffset','buffer']) Object.defineProperty(array,name,{get(){throw Error('page property consulted')}});
		if(crypto.getRandomValues(array)!==array)throw Error('identity lost');
		if(!backing.slice(16,144).some(value=>value!==0)||backing.slice(0,16).some(value=>value!==0)||backing.slice(144).some(value=>value!==0))throw Error('actual raw region lost');
		const OriginalException=DOMException;
		const originalCodes=OriginalException._codes;
		OriginalException._codes={TypeMismatchError:99};
		globalThis.DOMException=function(){throw Error('page constructor consulted')};
		try {
			let failure;
			try {crypto.getRandomValues([])} catch(error){failure=error}
			if(!(failure instanceof OriginalException)||failure.name!=='TypeMismatchError'||failure.code!==17)throw Error('captured exception owner lost');
		} finally {globalThis.DOMException=OriginalException;OriginalException._codes=originalCodes}
		let missing;
		try{crypto.getRandomValues()}catch(error){missing=error}
		if(!(missing instanceof TypeError))throw Error('missing argument did not throw actual TypeError');
		let shared;
		try{crypto.getRandomValues(new Uint8Array(new SharedArrayBuffer(8)))}catch(error){shared=error}
		if(!shared)throw Error('shared backing admitted');
	})()`); err != nil {
		t.Fatal(err)
	}
}
