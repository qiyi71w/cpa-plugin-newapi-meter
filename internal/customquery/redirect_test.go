package customquery

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound/hosttest"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

// Placeholder values for the redirect tests: distinctive, so a dump of what
// the other origin received can be searched for them.
const (
	redirAPIKey = "sk-synthetic-redirect-api-key-6201"
	redirToken  = "synthetic-redirect-access-token-6202"
	redirUser   = "synthetic-redirect-user-6203"
)

// redirScript puts one placeholder in a header, one in the URL query and
// one in the body.
const redirScript = `({
  request: {
    url: "{{baseUrl}}/v1/usage?token={{accessToken}}",
    method: "POST",
    headers: { "X-Key": "{{apiKey}}", "Content-Type": "application/json" },
    body: JSON.stringify({ user: "{{userId}}" })
  },
  extractor: function (r) { return { remaining: r.remaining, unit: "USD" }; }
})`

type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
}

func newRecorder(t *testing.T, h http.HandlerFunc) *recorder {
	rec := &recorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		var d strings.Builder
		d.WriteString(r.Method + " " + r.URL.RequestURI() + "\n")
		_ = r.Header.Write(&d)
		d.Write(b)
		rec.mu.Lock()
		rec.seen = append(rec.seen, d.String())
		rec.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (rec *recorder) dump() string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return strings.Join(rec.seen, "\n---\n")
}

func redirAccount(base string) store.Account {
	return store.Account{
		Source: store.SourceCustom, Proxy: store.ProxyDirect, CustomScript: redirScript,
		CustomPlaceholders: store.Placeholders{BaseURL: base, APIKey: redirAPIKey, AccessToken: redirToken, UserID: redirUser},
	}
}

func TestCrossOriginRedirectNeverReceivesPlaceholderValues(t *testing.T) {
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		other := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"remaining": 99}`))
		})
		site := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+"/collect", code)
		})
		rn, _ := newRunner()
		r := rn.Run(context.Background(), redirAccount(site.URL))
		got := other.dump()
		for _, v := range []string{redirAPIKey, redirToken, redirUser} {
			if strings.Contains(got, v) {
				t.Fatalf("%d: the other origin received %q:\n%s", code, v, got)
			}
		}
		// The redirect is reported with an existing failure class.
		wantFailure(t, r, status.BadResponse, "HTTP_"+strconv.Itoa(code))
	}
}

func TestSameOriginRedirectStillRunsTheQuery(t *testing.T) {
	var body string
	site := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/usage" {
			http.Redirect(w, r, "/v2/usage?"+r.URL.RawQuery, http.StatusPermanentRedirect)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		if r.Header.Get("X-Key") != redirAPIKey || r.URL.Query().Get("token") != redirToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"remaining": 7}`))
	})
	rn, _ := newRunner()
	r := rn.Run(context.Background(), redirAccount(site.URL))
	if r.Failure != nil || len(r.Items) != 1 || *r.Items[0].Remaining != 7 || !strings.Contains(body, redirUser) {
		t.Fatalf("result %+v (%s), body %q", r.Failure, r.Message, body)
	}
}

// Under follow, CPA's host follows the redirect itself. A script that keeps
// its secrets in credential-like header names and the URL query does not
// hand them to the other origin when the site's Location carries neither:
// the host drops those headers and the plugin's Referer names only the
// site's origin. (Other header names, a 307/308 body and a Location that
// repeats the query are outside what the host lets the plugin control.)
func TestFollowCrossOriginRedirectKeepsHeaderAndQueryValues(t *testing.T) {
	const script = `({
  request: { url: "{{baseUrl}}/v1/usage?token={{accessToken}}", headers: { "X-Api-Key": "{{apiKey}}" } },
  extractor: function (r) { return { remaining: r.remaining, unit: "USD" }; }
})`
	other := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"remaining": 99}`)) })
	site := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/collect", http.StatusFound)
	})
	rn := &Runner{HTTP: outbound.New(hosttest.New()), Now: func() time.Time { return now }}
	a := redirAccount(site.URL)
	a.Proxy, a.CustomScript = store.ProxyFollow, script
	rn.Run(context.Background(), a)
	got := other.dump()
	if got == "" {
		t.Fatal("the host did not follow the redirect")
	}
	for _, v := range []string{redirAPIKey, redirToken} {
		if strings.Contains(got, v) {
			t.Fatalf("the other origin received %q:\n%s", v, got)
		}
	}
	if !strings.Contains(got, "\r\nReferer: "+site.URL+"/\r\n") {
		t.Fatalf("Referer is not the site's origin:\n%s", got)
	}
}
