package js

import "testing"

func TestSyntheticCanvasAndWorkersDoNotProvideRealEffects(t *testing.T) {
	c := newCtxFromHTML(t, "<html><body><canvas id='canvas'></canvas></body></html>", nil)
	got, err := c.EvalString(t.Context(), `(()=>{
		const canvas=document.getElementById('canvas'), context=canvas.getContext('2d');
		context.fillStyle='red';context.fillRect(0,0,1,1);
		const pixels=Array.from(context.getImageData(0,0,1,1).data).join(',');
		let delivered=0;
		for(const Constructor of [Worker,SharedWorker]){
			const worker=new Constructor('data:text/javascript,postMessage("real-worker")');
			worker.onmessage=()=>{delivered++};
			worker.addEventListener('message',()=>{delivered++});
			worker.postMessage('actual input');
			worker.dispatchEvent(new MessageEvent('message',{data:'actual dispatch'}));
			worker.terminate();
		}
		return pixels+':'+delivered;
	})()`)
	if err != nil || got != "0,0,0,0:0" {
		t.Fatalf("synthetic contract changed; its classification requires review: %q %v", got, err)
	}
}
