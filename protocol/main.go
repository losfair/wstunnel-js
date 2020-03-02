package main

import (
	"flag"
	"fmt"
	"log"
	"syscall/js"
)

var globalWs js.Value

func setupWebsocket(url string) {
	globalWs = js.Global().Get("WebSocket").New(url)
	globalWs.Set("binaryType", "arraybuffer")
	globalWs.Set("onmessage", js.FuncOf(onWsMessage))
	globalWs.Set("onclose", js.FuncOf(onWsClose))
	globalWs.Set("onopen", js.FuncOf(onWsOpen))
	globalWs.Set("onerror", js.FuncOf(onWsError))
}

func main() {
	wsUrl := flag.String("remote", "", "WebSocket URL")
	flag.Parse()

	setupWebsocket(*wsUrl)

	root := js.ValueOf(map[string]interface{}{
		"socket": js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			return asynchronously(func() interface{} {
				return float64(JsSocket(args[0].String(), args[1].String()))
			})
		}),
		"connect": js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			return asynchronously(func() interface{} {
				JsConnect(SocketID(args[0].Float()), args[1].String())
				return nil
			})
		}),
		"close": js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			return asynchronously(func() interface{} {
				JsClose(SocketID(args[0].Float()))
				return nil
			})
		}),
		"send": js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			return asynchronously(func() interface{} {
				remoteAddr := ""
				if len(args) >= 3 {
					remoteAddr = args[2].String()
				}
				return JsSend(SocketID(args[0].Float()), args[1], remoteAddr)
			})
		}),
		"recv": js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			return asynchronously(func() interface{} {
				return JsRecv(SocketID(args[0].Float()), args[1])
			})
		}),
	})
	js.Global().Set("wstProtocol", root)
	select {}
}

func catchPanic() {
	if err := recover(); err != nil {
		log.Println("Error in protocol library:")
		log.Println(err)
	}
}

func asynchronously(f func() interface{}) js.Value {
	var cb js.Func
	cb = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		cb.Release()
		resolve, reject := args[0], args[1]
		go func() {
			defer func() {
				if err := recover(); err != nil {
					reject.Invoke(fmt.Sprintf("error in protocol library: %+v", err))
				}
			}()

			resolve.Invoke(f())
		}()
		return nil
	})
	return js.Global().Get("Promise").New(cb)
}
