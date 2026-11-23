"""Native C ABI fixture: loads the real plugin library the way CPA does and
drives register, management routes and shutdown against a synthetic new-api
site. It is a loading smoke test, not a CPA integration test.

usage: python3 tests/native_abi.py dist/<goos>-<goarch>/newapi-meter.<so|dll|dylib>
"""
import base64
import ctypes as c
import json
import os
import stat
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PAT = "nap_synthetic_fixture_token_000000001"
ROUTE = "/v0/management/plugins/newapi-meter"
PAGE = "/v0/resource/plugins/newapi-meter/index.html"


class Buffer(c.Structure):
    _fields_ = [("ptr", c.c_void_p), ("len", c.c_size_t)]


CALL = c.CFUNCTYPE(c.c_int, c.c_char_p, c.c_void_p, c.c_size_t, c.POINTER(Buffer))
FREE = c.CFUNCTYPE(None, c.c_void_p, c.c_size_t)
SHUTDOWN = c.CFUNCTYPE(None)


class HostAPI(c.Structure):
    _fields_ = [("abi_version", c.c_uint32), ("host_ctx", c.c_void_p), ("call", c.c_void_p), ("free_buffer", c.c_void_p)]


class PluginAPI(c.Structure):
    _fields_ = [("abi_version", c.c_uint32), ("call", CALL), ("free_buffer", FREE), ("shutdown", SHUTDOWN)]


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
    host, api = HostAPI(abi_version=1), PluginAPI()
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
                      ("DELETE", ROUTE + "/accounts"), ("GET", ROUTE + "/overview"), ("GET", ROUTE + "/detail")}, routes

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
        status, _, body = handle("GET", ROUTE + "/detail", {"id": [account_id], "period": ["today"]})
        assert status == 200 and PAT.encode() not in body
        detail = json.loads(body)
        assert detail["spend"] == 1 and detail["cached"] and detail["period"]["granularity"] == "hour", detail
        if os.name != "nt":
            assert stat.S_IMODE(os.stat(data_dir).st_mode) == 0o700
            assert stat.S_IMODE(os.stat(os.path.join(data_dir, "accounts.json")).st_mode) == 0o600
        status, _, _ = handle("DELETE", ROUTE + "/accounts", {"id": [account_id]})
        assert status == 200
        print("PASS management routes: create, masked list, overview and today's detail against synthetic site, delete")
    finally:
        site.shutdown()

    call("plugin.shutdown", {})
    api.shutdown()
    print("PASS shutdown")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    main(sys.argv[1])
