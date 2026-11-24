"""Native C ABI fixture: loads the real plugin library the way CPA does and
drives register, management routes and shutdown against a synthetic new-api
site. A fake host answers the host.http.* callbacks, so accounts that follow
CPA's proxy reach the site through the host API and direct ones do not. It is
a loading smoke test, not a CPA integration test.

usage: python3 tests/native_abi.py dist/<goos>-<goarch>/newapi-meter.<so|dll|dylib>
"""
import base64
import ctypes as c
import http.client
import json
import os
import stat
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

PAT = "nap_synthetic_fixture_token_000000001"
ROUTE = "/v0/management/plugins/newapi-meter"
PAGE = "/v0/resource/plugins/newapi-meter/index.html"


class Buffer(c.Structure):
    _fields_ = [("ptr", c.c_void_p), ("len", c.c_size_t)]


CALL = c.CFUNCTYPE(c.c_int, c.c_char_p, c.c_void_p, c.c_size_t, c.POINTER(Buffer))
FREE = c.CFUNCTYPE(None, c.c_void_p, c.c_size_t)
SHUTDOWN = c.CFUNCTYPE(None)
HOST_CALL = c.CFUNCTYPE(c.c_int, c.c_void_p, c.c_char_p, c.c_void_p, c.c_size_t, c.POINTER(Buffer))
HOST_FREE = c.CFUNCTYPE(None, c.c_void_p, c.c_size_t)


class HostAPI(c.Structure):
    _fields_ = [("abi_version", c.c_uint32), ("host_ctx", c.c_void_p), ("call", c.c_void_p), ("free_buffer", c.c_void_p)]


class PluginAPI(c.Structure):
    _fields_ = [("abi_version", c.c_uint32), ("call", CALL), ("free_buffer", FREE), ("shutdown", SHUTDOWN)]


class FakeHost:
    """Answers host.http.operation_open / do_stream / stream_read /
    stream_close / cancel like CPA does, sending each request itself without
    a proxy, and records the URLs and callback IDs it was asked to fetch."""

    def __init__(self):
        self.lock = threading.Lock()
        self.next = 0
        self.ops = {}
        self.streams = {}
        self.buffers = {}
        self.requests = []
        self.call_fn = HOST_CALL(self._call)
        self.free_fn = HOST_FREE(self._free)
        self.ctx = c.c_int(1)

    def api(self):
        return HostAPI(abi_version=1, host_ctx=c.cast(c.pointer(self.ctx), c.c_void_p),
                       call=c.cast(self.call_fn, c.c_void_p), free_buffer=c.cast(self.free_fn, c.c_void_p))

    def _id(self):
        with self.lock:
            self.next += 1
            return str(self.next)

    def _call(self, _ctx, method, req, n, out):
        try:
            env = {"ok": True, "result": self.handle(method.decode(), json.loads(c.string_at(req, n)) if n else {})}
        except Exception as e:  # reported to the plugin like a host failure
            env = {"ok": False, "error": {"code": "host_call_failed", "message": str(e)}}
        raw = json.dumps(env).encode()
        buf = c.create_string_buffer(raw, len(raw))
        with self.lock:
            self.buffers[c.addressof(buf)] = buf
        out[0].ptr = c.addressof(buf)
        out[0].len = len(raw)
        return 0

    def _free(self, ptr, _n):
        with self.lock:
            self.buffers.pop(ptr, None)

    def handle(self, method, req):
        if method == "host.http.operation_open":
            op = self._id()
            with self.lock:
                self.ops[op] = req.get("host_callback_id", "")
            return {"operation_id": op}
        if method == "host.http.cancel":
            with self.lock:
                self.ops.pop(req["operation_id"], None)
            return {}
        if method == "host.http.do_stream":
            with self.lock:
                callback = self.ops.pop(req.get("operation_id", ""), None)
            if callback is None or callback != req.get("host_callback_id", ""):
                raise ValueError("host http operation is not open")
            u = urlsplit(req["url"])
            conn = http.client.HTTPConnection(u.hostname, u.port, timeout=10)
            headers = {k: ", ".join(v) for k, v in (req.get("headers") or {}).items()}
            conn.request(req.get("method") or "GET", u.path + ("?" + u.query if u.query else ""),
                         body=base64.b64decode(req["body"]) if req.get("body") else None, headers=headers)
            resp = conn.getresponse()
            sid = self._id()
            with self.lock:
                self.streams[sid] = (conn, resp)
                self.requests.append((callback, req["url"]))
            grouped = {}
            for k, v in resp.getheaders():
                grouped.setdefault(k, []).append(v)
            return {"status_code": resp.status, "headers": grouped, "stream_id": sid}
        if method == "host.http.stream_read":
            with self.lock:
                conn, resp = self.streams[req["stream_id"]]
            data = resp.read(32 << 10)
            if data:
                return {"payload": base64.b64encode(data).decode()}
            self.handle("host.http.stream_close", req)
            return {"done": True}
        if method == "host.http.stream_close":
            with self.lock:
                entry = self.streams.pop(req["stream_id"], None)
            if entry:
                entry[0].close()
            return {}
        raise ValueError("unsupported host callback " + method)


class Site(BaseHTTPRequestHandler):
    """Synthetic new-api site: $10 balance, $1 spent today."""

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        data = {
            "/api/status": {"quota_per_unit": 500000, "quota_display_type": "USD", "version": "v0.0.0-fixture", "enable_data_export": True},
            "/api/user/self": {"quota": 5000000, "used_quota": 1000000},
            "/api/log/self/stat": {"quota": 500000, "rpm": 1, "tpm": 10},
            "/api/data/self": [],
        }.get(path)
        if path != "/api/status" and self.headers.get("Authorization") != "Bearer " + PAT:
            self.send_response(401)
            data = None
        else:
            self.send_response(200 if data is not None else 404)
        body = json.dumps({"success": data is not None, "data": data}).encode()
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


def main(path):
    lib = c.CDLL(os.path.abspath(path))
    lib.cliproxy_plugin_init.argtypes = [c.POINTER(HostAPI), c.POINTER(PluginAPI)]
    lib.cliproxy_plugin_init.restype = c.c_int
    fake = FakeHost()
    host, api = fake.api(), PluginAPI()
    assert lib.cliproxy_plugin_init(c.byref(host), c.byref(api)) == 0
    assert api.abi_version == 1
    assert lib.cliproxy_plugin_init(c.byref(HostAPI(abi_version=2)), c.byref(PluginAPI())) != 0, "accepted ABI 2"

    def call(method, obj):
        raw = json.dumps(obj).encode()
        out = Buffer()
        assert api.call(method.encode(), c.cast(c.c_char_p(raw), c.c_void_p), len(raw), c.byref(out)) == 0, method
        try:
            env = json.loads(c.string_at(out.ptr, out.len))
        finally:
            api.free_buffer(out.ptr, out.len)
        assert env["ok"], env
        return env["result"]

    def handle(method, route, query=None, body=None):
        req = {"Method": method, "Path": route, "Query": query or {}, "Headers": {}, "host_callback_id": "fixture"}
        if body is not None:
            req["Body"] = base64.b64encode(json.dumps(body).encode()).decode()
        res = call("management.handle", req)
        return res["StatusCode"], res["Headers"], base64.b64decode(res.get("Body") or "")

    data_dir = tempfile.mkdtemp(prefix="newapi-meter-abi-")
    data_dir = os.path.join(data_dir, "data")
    config = base64.b64encode(f"enabled: true\npriority: 0\ndata_dir: '{data_dir}'\n".encode()).decode()

    reg = call("plugin.register", {"config_yaml": config, "schema_version": 10})
    assert reg["schema_version"] == 6, reg
    assert reg["capabilities"] == {"management_api": True}, reg["capabilities"]
    assert [f["Name"] for f in reg["metadata"]["ConfigFields"]] == ["data_dir"]
    print("PASS register: schema_version 6, capabilities only management_api, ConfigFields data_dir")

    mreg = call("management.register", {"BasePath": "/v0/management", "ResourceBasePath": "/v0/resource/plugins/newapi-meter"})
    assert [r["Path"] for r in mreg["resources"]] == [PAGE]
    assert mreg["resources"][0]["Menu"] == "余额用量 · NewAPI Meter"
    routes = {(r["Method"], r["Path"]) for r in mreg["routes"]}
    assert routes == {("GET", ROUTE + "/accounts"), ("POST", ROUTE + "/accounts"), ("PUT", ROUTE + "/accounts"),
                      ("DELETE", ROUTE + "/accounts"), ("GET", ROUTE + "/overview"),
                      ("POST", ROUTE + "/accounts/test"), ("GET", ROUTE + "/detail")}, routes

    status, headers, body = handle("GET", PAGE)
    assert status == 200 and headers["Content-Type"] == ["text/html; charset=utf-8"], (status, headers)
    assert body.startswith(b"<!doctype html>") and b'<div id="app">' in body and b"<script>" in body
    assert "script-src 'sha256-" in headers["Content-Security-Policy"][0]
    print(f"PASS resource route: static page ({len(body)} bytes) with CSP")

    site = ThreadingHTTPServer(("127.0.0.1", 0), Site)
    threading.Thread(target=site.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{site.server_address[1]}"
    try:
        status, _, body = handle("POST", ROUTE + "/accounts", body={"name": "Fixture", "base_url": base + "/", "credential": PAT, "low_balance_threshold": 2})
        assert status == 201, body
        account_id = json.loads(body)["account"]["id"]
        status, _, body = handle("GET", ROUTE + "/accounts")
        assert status == 200 and PAT.encode() not in body
        listed = json.loads(body)["accounts"][0]
        assert listed["credential_masked"] and listed["base_url"] == base
        status, _, body = handle("GET", ROUTE + "/overview", {"force": ["1"]})
        assert status == 200 and PAT.encode() not in body
        item = json.loads(body)["accounts"][0]
        assert item["state"] == "ok" and item["balance"] == 10 and item["today_spend"] == 1, item
        # The account follows CPA's proxy (the default): its four requests went through the host API.
        assert len(fake.requests) == 4 and all(cb == "fixture" and u.startswith(base + "/api/") for cb, u in fake.requests), fake.requests
        before = open(os.path.join(data_dir, "accounts.json"), "rb").read()
        status, _, body = handle("POST", ROUTE + "/accounts/test", body={"id": account_id, "base_url": base, "credential": ""})
        assert status == 200 and PAT.encode() not in body, body
        tested = json.loads(body)
        assert tested["ok"] and tested["credential_kind"] == "pat" and tested["currency"]["code"] == "USD", tested
        assert open(os.path.join(data_dir, "accounts.json"), "rb").read() == before
        assert len(fake.requests) == 8, fake.requests
        status, _, body = handle("GET", ROUTE + "/detail", {"id": [account_id], "period": ["today"]})
        assert status == 200 and PAT.encode() not in body
        detail = json.loads(body)
        assert detail["spend"] == 1 and detail["cached"] and detail["period"]["granularity"] == "hour", detail
        if os.name != "nt":
            assert stat.S_IMODE(os.stat(data_dir).st_mode) == 0o700
            assert stat.S_IMODE(os.stat(os.path.join(data_dir, "accounts.json")).st_mode) == 0o600
        print("PASS management routes: create, masked list, overview, connection test and today's detail against synthetic site through the host API")
        status, _, body = handle("PUT", ROUTE + "/accounts", body={"id": account_id, "name": "Fixture", "base_url": base, "credential": "",
                                                                   "low_balance_threshold": 2, "proxy": "direct"})
        assert status == 200, body
        status, _, body = handle("GET", ROUTE + "/overview", {"force": ["1"]})
        item = json.loads(body)["accounts"][0]
        assert status == 200 and item["state"] == "ok" and item["balance"] == 10, item
        assert len(fake.requests) == 8, fake.requests
        assert not fake.ops and not fake.streams, (fake.ops, fake.streams)
        status, _, _ = handle("DELETE", ROUTE + "/accounts", {"id": [account_id]})
        assert status == 200
        print("PASS proxy option: follow requests use host.http.* with the management callback ID, direct ones bypass the host; delete")
    finally:
        site.shutdown()

    call("plugin.shutdown", {})
    api.shutdown()
    print("PASS shutdown")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    main(sys.argv[1])
