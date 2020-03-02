importScripts("wasm_exec.js");
importScripts("mutex.js");

let initCalled = false;
let go = new Go();
let instance = null;
let clients = {};
let nextClientId = 1;
let messageMu = new Mutex();

class Client {
    constructor({ memory }) {
        this.memory = memory;
    }
}

self.addEventListener("message", async e => {
    let unlock = await messageMu.lock();
    let reply = {
        requestId: e.data.requestId,
    };
    try {
        switch(e.data.type) {
            case "init": {
                if(initCalled) throw new Error("init called twice");

                const result = await WebAssembly.instantiateStreaming(fetch("protocol.wasm"), go.importObject);
                instance = result.instance;

                go.argv = [
                    "protocol",
                    "--remote",
                    e.data.tunnel,
                ];
                go.run(instance);

                initCalled = true;
                console.log("worker initialized");

                break;
            }
            case "session_open": {
                assertInit();

                let memory = e.data.memory;
                assertObjType(memory, SharedArrayBuffer);

                let id = nextClientId++;
                clients[id] = new Client( { memory: memory });
                reply.sessionId = id;
                break;
            }
            case "session_close": {
                assertInit();

                delete clients[e.data.sessionId];
                break;
            }
            case "session_update_memory": {
                assertInit();

                let client = clients[e.data.sessionId];
                assertObjType(client, Client);

                let memory = e.data.memory;
                assertObjType(memory, SharedArrayBuffer);

                client.memory = memory;
                break;
            }
            case "socket": {
                assertInit();
                
                let sab = e.data.sab;
                assertObjType(sab, SharedArrayBuffer);

                try {
                    let handle = await self.wstProtocol.socket(e.data.network, e.data.transport);
                    Atomics.store(new Int32Array(sab, 4), 0, handle);
                } catch(e) {
                    console.error(e);
                    Atomics.store(new Int32Array(sab, 4), 0, -1);
                }

                notifyShmem(new Int32Array(sab, 0));
                reply = null;
                break;
            }
            default: {
                throw new Error("invalid event type");
            }
        }
    } catch(e) {
        console.error("error in worker:", e);
        reply.error = "" + e;
    } finally {
        unlock();
        if(reply !== null) self.postMessage(reply);
    }
})

function assertInit() {
    if(!initCalled) {
        throw new Error("not yet initialized");
    }
}

function assertObjType(obj, type) {
    if(!(obj instanceof type)) {
        throw new TypeError("expecting " + type + ", got " + obj);
    }
}

function notifyShmem(array) {
    Atomics.store(array, 0, 1);
    Atomics.notify(array, 0, 1);
}