// Command newapi-meter builds the CPA native plugin (go build -buildmode=c-shared).
// It only adapts the CPA plugin C ABI 1 to internal/plugin, and the host API
// CPA hands to cliproxy_plugin_init to outbound.Host.
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
typedef int (*host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*host_free_fn)(void*, size_t);
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static int host_call(const cliproxy_host_api* h, const char* method, const uint8_t* in, size_t n, cliproxy_buffer* out) {
	return ((host_call_fn)h->call)(h->host_ctx, method, in, n, out);
}

static void host_free(const cliproxy_host_api* h, void* ptr, size_t n) {
	if (h->free_buffer != NULL) ((host_free_fn)h->free_buffer)(ptr, n);
}
*/
import "C"

import (
	"errors"
	"sync/atomic"
	"unsafe"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/plugin"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/webui"
)

// maxInput bounds a single host payload; management bodies are small JSON.
const maxInput = 4 << 20

// hostAPI is the host API received in cliproxy_plugin_init. CPA keeps the
// block alive for as long as the library is mapped.
type hostAPI struct {
	api atomic.Pointer[C.cliproxy_host_api]
}

var errNoHost = errors.New("host api unavailable")

// Call implements outbound.Host.
func (h *hostAPI) Call(method string, request []byte) ([]byte, error) {
	api := h.api.Load()
	if api == nil || api.call == nil {
		return nil, errNoHost
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var in unsafe.Pointer
	if len(request) > 0 {
		in = C.CBytes(request)
		defer C.free(in)
	}
	var out C.cliproxy_buffer
	rc := C.host_call(api, cMethod, (*C.uint8_t)(in), C.size_t(len(request)), &out)
	var data []byte
	if out.ptr != nil {
		data = C.GoBytes(out.ptr, C.int(out.len))
		C.host_free(api, out.ptr, out.len)
	}
	if rc != 0 {
		return nil, errors.New("host call failed")
	}
	return data, nil
}

var (
	host     hostAPI
	instance = plugin.New(plugin.Options{Page: webui.Page, HTTP: outbound.New(&host)})
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(api *C.cliproxy_host_api, out *C.cliproxy_plugin_api) C.int {
	if api == nil || out == nil || api.abi_version != 1 {
		return 1
	}
	host.api.Store(api)
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
