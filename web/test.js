let wst_wasm = new WstWasm({
    tunnel: "wss://wst.unsafe.world/",
    worker: "worker.js",
});
let worker = new Worker("test_worker.js");
worker.addEventListener("message", e => {
    console.log(e);
    wst_wasm.worker.postMessage(e.data);
});
wst_wasm.worker.addEventListener("message", e => {
    console.log(e);
    worker.postMessage(e.data);
});
