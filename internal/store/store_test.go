package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	pat    = "nap_synthetic_personal_access_token_0001"
	legacy = "synthetic-legacy-access-token-0002"
)

func ptr(v float64) *float64 { return &v }

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func patInput(name string) Input {
	return Input{Name: name, BaseURL: "https://site.example.com/", Credential: pat, LowBalanceThreshold: ptr(5)}
}

func TestCreateWritesCompleteV1Record(t *testing.T) {
	s := openTemp(t)
	in := Input{Name: " Main ", BaseURL: "https://site.example.com/sub//", Credential: legacy, UserID: "42", LowBalanceThreshold: ptr(3.5), Proxy: ProxyDirect}
	a, err := s.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Main" || a.BaseURL != "https://site.example.com/sub" || a.Source != SourceNewAPI || a.Order != 0 || !strings.HasPrefix(a.ID, "acc_") {
		t.Fatalf("account = %+v", a)
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["schema_version"] != float64(1) {
		t.Fatalf("schema_version = %v", doc["schema_version"])
	}
	rec := doc["accounts"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "order", "name", "source", "base_url", "credential", "user_id", "low_balance_threshold", "proxy", "custom_script", "custom_placeholders"} {
		if _, ok := rec[key]; !ok {
			t.Errorf("record lacks %q", key)
		}
	}
	ph := rec["custom_placeholders"].(map[string]any)
	for _, key := range []string{"baseUrl", "apiKey", "accessToken", "userId"} {
		if _, ok := ph[key]; !ok {
			t.Errorf("placeholders lack %q", key)
		}
	}
}

func TestDefaultsAndOrder(t *testing.T) {
	s := openTemp(t)
	for _, n := range []string{"a", "b", "c"} {
		if _, err := s.Create(patInput(n)); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.List()
	if err := s.Delete(list[1].ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.List()
	if len(list) != 2 || list[0].Name != "a" || list[1].Name != "c" || list[1].Order != 1 {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Proxy != ProxyFollow {
		t.Fatalf("default proxy = %q", list[0].Proxy)
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name   string
		in     Input
		field  string
		reason string
	}{
		{"name required", Input{BaseURL: "https://x.example.com", Credential: pat}, "name", ReasonRequired},
		{"base url required", Input{Name: "x", Credential: pat}, "base_url", ReasonRequired},
		{"ftp rejected", Input{Name: "x", BaseURL: "ftp://x.example.com", Credential: pat}, "base_url", ReasonScheme},
		{"no scheme", Input{Name: "x", BaseURL: "x.example.com", Credential: pat}, "base_url", ReasonInvalid},
		{"userinfo rejected", Input{Name: "x", BaseURL: "https://u:p@x.example.com", Credential: pat}, "base_url", ReasonInvalid},
		{"credential required", Input{Name: "x", BaseURL: "https://x.example.com"}, "credential", ReasonRequired},
		{"legacy needs user id", Input{Name: "x", BaseURL: "https://x.example.com", Credential: legacy}, "user_id", ReasonRequired},
		{"user id numeric", Input{Name: "x", BaseURL: "https://x.example.com", Credential: legacy, UserID: "12a"}, "user_id", ReasonNotNumeric},
		{"threshold non-negative", Input{Name: "x", BaseURL: "https://x.example.com", Credential: pat, LowBalanceThreshold: ptr(-1)}, "low_balance_threshold", ReasonNegative},
		{"proxy option", Input{Name: "x", BaseURL: "https://x.example.com", Credential: pat, Proxy: "socks"}, "proxy", ReasonInvalid},
		{"custom source not yet supported", Input{Name: "x", Source: SourceCustom, BaseURL: "https://x.example.com", Credential: pat}, "source", ReasonUnsupported},
	}
	s := openTemp(t)
	for _, tc := range cases {
		_, err := s.Create(tc.in)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != tc.field || ve.Reason != tc.reason {
			t.Errorf("%s: err = %v, want %s/%s", tc.name, err, tc.field, tc.reason)
		}
	}
	if _, err := s.Create(Input{Name: "pat", BaseURL: "http://x.example.com", Credential: pat, LowBalanceThreshold: ptr(0)}); err != nil {
		t.Fatalf("PAT without user id and zero threshold rejected: %v", err)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateKeepsBlankOrMaskedSecrets(t *testing.T) {
	s := openTemp(t)
	a, err := s.Create(Input{Name: "x", BaseURL: "https://x.example.com", Credential: legacy, UserID: "7"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Update(a.ID, Input{Name: "renamed", BaseURL: "https://x.example.com", UserID: "7"})
	if err != nil || b.Credential != legacy || b.Name != "renamed" {
		t.Fatalf("blank credential: %+v %v", b, err)
	}
	c, err := s.Update(a.ID, Input{Name: "renamed", BaseURL: "https://x.example.com", Credential: Mask(legacy), UserID: "7"})
	if err != nil || c.Credential != legacy {
		t.Fatalf("masked credential: %+v %v", c, err)
	}
	d, err := s.Update(a.ID, Input{Name: "renamed", BaseURL: "https://x.example.com", Credential: pat})
	if err != nil || d.Credential != pat || d.LowBalanceThreshold != nil {
		t.Fatalf("replaced credential: %+v %v", d, err)
	}
	if _, err := s.Update("acc_missing", patInput("y")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing id: %v", err)
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	s := openTemp(t)
	if _, err := s.Create(patInput("x")); err != nil {
		t.Fatal(err)
	}
	di, _ := os.Stat(s.Dir())
	fi, _ := os.Stat(s.Path())
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("dir %v file %v", di.Mode().Perm(), fi.Mode().Perm())
	}
}

func TestAtomicReplace(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Create(patInput("first")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path())
	if runtime.GOOS != "windows" {
		// A directory that refuses new files makes the temporary write fail:
		// the existing file must stay byte-for-byte intact.
		if err := os.Chmod(s.Dir(), 0o500); err != nil {
			t.Fatal(err)
		}
		_, err := s.Create(patInput("second"))
		_ = os.Chmod(s.Dir(), 0o700)
		var fe *FileError
		if !errors.As(err, &fe) || fe.Kind != FileUnavailable {
			if os.Geteuid() != 0 {
				t.Fatalf("write into read-only dir: %v", err)
			}
		}
		after, _ := os.ReadFile(s.Path())
		if os.Geteuid() != 0 && string(after) != string(before) {
			t.Fatal("failed write changed the data file")
		}
	}
	if _, err := s.Create(patInput("second")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.Dir())
	if len(entries) != 1 || entries[0].Name() != FileName {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v, want only %s", names, FileName)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v %v", list, err)
	}
}

func TestRefusesToOverwriteUnreadableFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		kind    FileErrorKind
	}{
		{"corrupt", `{"schema_version":1,"accounts":[`, FileCorrupt},
		{"not an object", `[]`, FileCorrupt},
		{"no schema version", `{"accounts":[]}`, FileCorrupt},
		{"newer schema", `{"schema_version":2,"accounts":[],"future":true}`, FileNewer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			if err := os.WriteFile(s.Path(), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			check := func(op string, err error) {
				var fe *FileError
				if !errors.As(err, &fe) || fe.Kind != tc.kind || fe.Path != s.Path() {
					t.Fatalf("%s: err = %v, want %s", op, err, tc.kind)
				}
			}
			_, err := s.List()
			check("list", err)
			_, err = s.Create(patInput("x"))
			check("create", err)
			_, err = s.Update("acc_x", patInput("x"))
			check("update", err)
			check("delete", s.Delete("acc_x"))
			got, _ := os.ReadFile(s.Path())
			if string(got) != tc.content {
				t.Fatalf("file was rewritten: %q", got)
			}
		})
	}
}

func TestMaskNeverRevealsWholeSecret(t *testing.T) {
	for _, secret := range []string{"short", "fifteen-chars-x", pat, legacy} {
		m := Mask(secret)
		if strings.Contains(m, secret) || len(m) == 0 {
			t.Fatalf("Mask(%q) = %q", secret, m)
		}
	}
	if got := Mask(pat); !strings.HasPrefix(got, "nap_") || !strings.HasSuffix(got, "0001") {
		t.Fatalf("Mask(pat) = %q", got)
	}
}
