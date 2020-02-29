package main

import (
	"encoding/json"
	"log"
	"syscall/js"
	"sync/atomic"
)

var gotNetconf int32 = 0

type NetConf struct {
	IPv4 IPConf `json:"ipv4"`
	IPv6 IPConf `json:"ipv6"`
}

type IPConf struct {
	Address string `json:"address"`
	Gateway string `json:"gateway"`
	PrefixLength uint8 `json:"prefix_length"`
}

func onWsMessage(this js.Value, args []js.Value) interface{} {
	defer catchPanic()

	message := args[0].Get("data")
	ty := message.Type()
	switch ty {
	case js.TypeString:
		// Configuration information
		var conf NetConf
		if err := json.Unmarshal([]byte(message.String()), &conf); err != nil {
			panic("cannot decode network configuration")
		}
		if atomic.SwapInt32(&gotNetconf, 1) != 0 {
			panic("got network configuration twice")
		}
		configureNetwork(conf)
	case js.TypeObject:
		// Max header size is 40 bytes (IPv6).
		if atomic.LoadInt32(&gotNetconf) != 1 {
			panic("got message before netconf")
		}
		message = js.Global().Get("Uint8Array").New(message)
		buf := make([]byte, message.Get("byteLength").Int())
		js.CopyBytesToGo(buf, message)
		dispatchPacket(buf)
	default:
		panic("invalid ws message type")
	}
	return nil
}

func onWsClose(this js.Value, args []js.Value) interface{} {
	defer catchPanic()
	log.Println("WebSocket close")
	return nil
}

func onWsOpen(this js.Value, args []js.Value) interface{} {
	defer catchPanic()
	log.Println("WebSocket opened")
	return nil
}

func onWsError(this js.Value, args []js.Value) interface{} {
	defer catchPanic()
	log.Println("WebSocket error")
	return nil
}