// Command newapi-meter builds the CPA native plugin (go build -buildmode=c-shared).
// It only adapts the CPA plugin C ABI 1 to internal/plugin.
package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef struct { uint32_t abi_version; void* host_ctx; void* call; void* free_buffer; } cliproxy_host_api;
typedef int (*call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*free_fn)(void*, size_t);
typedef void (*shutdown_fn)(void);
typedef struct { uint32_t abi_version; call_fn call; free_fn free_buffer; shutdown_fn shutdown; } cliproxy_plugin_api;
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"unsafe"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/plugin"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/webui"
)

// maxInput bounds a single host payload; management bodies are small JSON.
const maxInput = 4 << 20

var instance = plugin.New(plugin.Options{Page: webui.Page})

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, out *C.cliproxy_plugin_api) C.int {
	if host == nil || out == nil || host.abi_version != 1 {
		return 1
	}
	out.abi_version = 1
	out.call = C.call_fn(C.cliproxyPluginCall)
	out.free_buffer = C.free_fn(C.cliproxyPluginFree)
	out.shutdown = C.shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, input *C.uint8_t, n C.size_t, out *C.cliproxy_buffer) C.int {
	if out == nil {
		return 1
	}
	out.ptr = nil
	out.len = 0
	if method == nil || (input == nil && n != 0) || uint64(n) > maxInput {
		return 1
	}
	// Copy the host-owned bytes; nothing borrowed outlives this call.
	raw := C.GoBytes(unsafe.Pointer(input), C.int(n))
	data := instance.Call(C.GoString(method), raw)
	out.ptr = C.CBytes(data)
	out.len = C.size_t(len(data))
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, n C.size_t) { C.free(ptr) }

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() { instance.Shutdown() }
