package plugin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// hostURLs returns the URLs the fake host was asked to fetch since mark,
// and fails if any of them did not carry the management call's callback ID.
func (h *harness) hostURLs(mark int) []string {
	h.t.Helper()
	var out []string
	for _, r := range h.host.Requests()[mark:] {
		if r.HostCallbackID != "cb-test" || r.OperationID == "" {
			h.t.Fatalf("host request without the management call context: %+v", r)
		}
		out = append(out, r.URL)
	}
	return out
}

func countPrefix(urls []string, prefix string) int {
	n := 0
	for _, u := range urls {
		if strings.HasPrefix(u, prefix+"/") {
			n++
		}
	}
	return n
}

// Every request kind uses the account's proxy option: follow goes through
// the host (and so CPA's proxy-url), direct never touches the host.
func TestProxyOptionSelectsTheTransportForEveryRequestKind(t *testing.T) {
	h := newHarness(t)
	followSite, directSite := newUpstream(t), newUpstream(t)
	followCustom, directCustom := newCustomSite(t, `{"ok": true, "plans": [{"name": "p", "left": 1, "unit": "USD"}]}`),
		newCustomSite(t, `{"ok": true, "plans": [{"name": "p", "left": 2, "unit": "USD"}]}`)

	followID := h.create(followSite, "Follow", secretPAT, nil) // proxy omitted: follow is the default
	r := h.do("POST", RoutePrefix+"/accounts", nil, map[string]any{"name": "Direct", "base_url": directSite.URL, "credential": secretPAT, "proxy": "direct"})
	if r.Status != http.StatusCreated {
		t.Fatalf("create direct: %d %s", r.Status, r.Body)
	}
	directID := r.json(t)["account"].(map[string]any)["id"].(string)
	in := customInput("Follow custom", followCustom.URL, nil)
	in["proxy"] = "follow"
	if r := h.do("POST", RoutePrefix+"/accounts", nil, in); r.Status != http.StatusCreated {
		t.Fatalf("create follow custom: %d %s", r.Status, r.Body)
	}
	h.createCustom("Direct custom", directCustom.URL, nil)

	check := func(what string, mark int, follow map[string]int, direct ...string) {
		t.Helper()
		urls := h.hostURLs(mark)
		total := 0
		for prefix, want := range follow {
			if got := countPrefix(urls, prefix); got != want {
				t.Fatalf("%s: %d host requests to the follow site, want %d (%v)", what, got, want, urls)
			}
			total += want
		}
		for _, prefix := range direct {
			if countPrefix(urls, prefix) != 0 {
				t.Fatalf("%s: a direct account went through the host: %v", what, urls)
			}
		}
		if len(urls) != total {
			t.Fatalf("%s: unexpected host requests %v", what, urls)
		}
	}

	mark := len(h.host.Requests())
	items := h.overview(url.Values{"force": {"1"}})
	for _, it := range items {
		if it["state"] != "ok" {
			t.Fatalf("overview %v", it)
		}
	}
	check("overview", mark, map[string]int{followSite.URL: 4, followCustom.URL: 1}, directSite.URL, directCustom.URL)
	if directSite.count("/api/user/self") != 1 || directCustom.hits.Load() != 1 {
		t.Fatal("direct accounts did not reach their sites")
	}
	for _, req := range h.host.Requests()[mark:] {
		if strings.HasSuffix(strings.SplitN(req.URL, "?", 2)[0], "/api/user/self") && strings.Join(req.Headers["Authorization"], "") != "Bearer "+secretPAT {
			t.Fatalf("follow request lost its credential header: %v", req.Headers)
		}
	}

	mark = len(h.host.Requests())
	h.detail(url.Values{"id": {followID}, "period": {"7d"}, "force": {"1"}})
	h.detail(url.Values{"id": {directID}, "period": {"7d"}, "force": {"1"}})
	check("detail", mark, map[string]int{followSite.URL: 3}, directSite.URL)

	mark = len(h.host.Requests())
	for _, proxy := range []string{"follow", "direct"} {
		site := map[string]*upstream{"follow": followSite, "direct": directSite}[proxy]
		if st, res := h.testConnection(map[string]any{"name": "T", "base_url": site.URL, "credential": secretPAT, "proxy": proxy}); st != 200 || res["ok"] != true {
			t.Fatalf("test connection %s: %d %v", proxy, st, res)
		}
	}
	check("test connection", mark, map[string]int{followSite.URL: 4}, directSite.URL)

	mark = len(h.host.Requests())
	for _, proxy := range []string{"follow", "direct"} {
		site := map[string]*customSite{"follow": followCustom, "direct": directCustom}[proxy]
		in := customInput("T", site.URL, nil)
		in["proxy"] = proxy
		if st, res := h.testConnection(in); st != 200 || res["ok"] != true {
			t.Fatalf("test script %s: %d %v", proxy, st, res)
		}
	}
	check("test script", mark, map[string]int{followCustom.URL: 1}, directCustom.URL)

	if n := h.host.Open(); n != 0 {
		t.Fatalf("%d host operations left open", n)
	}
}
