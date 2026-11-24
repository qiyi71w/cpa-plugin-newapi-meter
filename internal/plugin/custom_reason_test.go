package plugin

import (
	"testing"
)

// SEC-8: a script that installs an inherited toJSON makes the prelude report
// {"reason": <apiKey>}. Neither the test-script answer nor the overview may
// carry it: the reason becomes the plugin's fixed "unknown" code.
func TestForgedScriptReasonNeverReachesResponses(t *testing.T) {
	h := newHarness(t)
	site := newCustomSite(t, `{"ok": true}`)
	script := "Object.prototype.toJSON = function () { return { reason: \"{{apiKey}}\" }; };\n" +
		"({ request: { url: \"{{baseUrl}}/x\" }, extractor: function (r) { return r; } })"
	reasonCode := func(custom any) any {
		c, _ := custom.(map[string]any)
		r, _ := c["script_reason"].(map[string]any)
		return r["code"]
	}

	in := customInput("", site.URL, nil)
	in["custom_script"] = script
	r := h.do("POST", RoutePrefix+"/accounts/test", nil, in)
	assertNoCustomSecrets(t, "test response", string(r.Body))
	if res := r.json(t); reasonCode(res["custom"]) != "unknown" {
		t.Fatalf("test-script = %v", res)
	}

	in["name"] = "Forged"
	if c := h.do("POST", RoutePrefix+"/accounts", nil, in); c.Status != 201 {
		t.Fatalf("create: %d %s", c.Status, c.Body)
	}
	o := h.do("GET", RoutePrefix+"/overview", nil, nil)
	assertNoCustomSecrets(t, "overview response", string(o.Body))
	items := h.overview(nil)
	if reasonCode(items[0]["custom"]) != "unknown" || items[0]["state"] != "needs_action" {
		t.Fatalf("overview = %v", items[0])
	}
	assertNoCustomSecrets(t, "plugin log", h.logs.String())
}
