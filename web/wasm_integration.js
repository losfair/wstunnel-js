(function() {
    const OP_NULL = 0;
    const OP_READ = 1;
    const OP_WRITE = 2;
    const OP_CONNECT = 3;
    const OP_ACCEPT = 4;

    const NET_MAP = {
        0: "ip4",
        1: "ip6",
    }

    const TRANSPORT_MAP = {
        0: "tcp",
        1: "udp",
    }

    class WstWasm {
        constructor({ worker, tunnel }) {
            if(
                typeof(worker) != "string" || 
                typeof(tunnel) != "string"
            ) throw new TypeError();

            this.worker = new Worker(worker);
            this.worker.postMessage({
                type: "init",
                tunnel: tunnel,
            });
        }
    }

    class WstWasmInstance {
        constructor(waInst) {
            //if(!(waInst instanceof WebAssembly.Instance)) throw new TypeError();
            this.waInst = waInst;
        }

        getImportObject() {
            return {
                "socket": this._socket.bind(this),
                "ring": this._ring.bind(this),
                "enter": this._enter.bind(this),
            };
        }

        _socket(network, transport) {
            network = NET_MAP[network]
            transport = TRANSPORT_MAP[transport]
            if(!network || !transport) throw new Error("invalid network or transport");

            let sab = new SharedArrayBuffer(8);

            self.postMessage({
                type: "socket",
                sab: sab,
                network: network,
                transport: transport,
            });
            Atomics.wait(new Int32Array(sab, 0), 0, 0);
            return (new Int32Array(sab, 4))[0];
        }

        _ring() {
            throw new Error("not implemented");
        }

        _enter() {
            throw new Error("not implemented");
        }
    }

    self.WstWasm = WstWasm;
    self.WstWasmInstance = WstWasmInstance;
})()
