importScripts("wasm_integration.js");

let inst = new WstWasmInstance(null);
console.log("inst created");
let imports = inst.getImportObject();
console.log(imports);

let socket = imports.socket(0, 0);
console.log("socket created.", socket)
socket = imports.socket(0, 0);
console.log("socket created.", socket)

self.addEventListener("message", async e => {

});