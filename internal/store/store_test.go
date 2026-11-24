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
		{"custom script required", Input{Name: "x", Source: SourceCustom, Credential: pat}, "custom_script", ReasonRequired},
		{"custom baseUrl scheme", Input{Name: "x", Source: SourceCustom, CustomScript: "({})", CustomPlaceholders: Placeholders{BaseURL: "ftp://x.example.com"}}, "custom_placeholders.baseUrl", ReasonScheme},
		{"custom baseUrl query", Input{Name: "x", Source: SourceCustom, CustomScript: "({})", CustomPlaceholders: Placeholders{BaseURL: "https://x.example.com/?a=1"}}, "custom_placeholders.baseUrl", ReasonInvalid},
		{"unknown source", Input{Name: "x", Source: "other", BaseURL: "https://x.example.com", Credential: pat}, "source", ReasonInvalid},
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

func TestCustomAccountKeepsSecretsAndTakesHostFromBaseURL(t *testing.T) {
	const key, token = "sk-synthetic-custom-api-key-0003", "synthetic-custom-access-token-0004"
	s := openTemp(t)
	in := Input{
		Name: "Custom", Source: SourceCustom, CustomScript: "({ request: {}, extractor: function () {} })",
		// Built-in fields do not apply to a custom query and are dropped.
		BaseURL: "https://ignored.example.com", Credential: pat, UserID: "9",
		CustomPlaceholders: Placeholders{BaseURL: " https://api.example.com/v1/ ", APIKey: key, AccessToken: token, UserID: "u-1"},
	}
	a, err := s.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	ph := a.CustomPlaceholders
	if a.BaseURL != "https://api.example.com/v1" || ph.BaseURL != a.BaseURL || a.Credential != "" || a.UserID != "" || ph.UserID != "u-1" {
		t.Fatalf("account = %+v", a)
	}
	// Blank and masked secrets keep the stored values; script and readable values are replaced.
	in.CustomScript, in.CustomPlaceholders = "({ edited: true })", Placeholders{APIKey: "", AccessToken: Mask(token), UserID: "u-2"}
	b, err := s.Update(a.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if b.CustomPlaceholders.APIKey != key || b.CustomPlaceholders.AccessToken != token || b.CustomPlaceholders.UserID != "u-2" || b.CustomScript != "({ edited: true })" || b.BaseURL != "" {
		t.Fatalf("updated = %+v", b)
	}
	// Resolve (connection test) applies the same rules without writing.
	r, err := Resolve(Input{Source: SourceCustom, CustomScript: "({})"}, &b)
	if err != nil || r.CustomPlaceholders.APIKey != key || r.CustomPlaceholders.AccessToken != token {
		t.Fatalf("resolve = %+v %v", r, err)
	}
}

// Saving as built-in drops the custom-query secrets, as saving as custom
// drops the built-in credential, so a later switch back with blank fields
// starts without them instead of reusing secrets the form no longer shows.
func TestSwitchingSourceDropsTheOtherSourcesSecrets(t *testing.T) {
	const key, token = "sk-synthetic-custom-api-key-0005", "synthetic-custom-access-token-0006"
	s := openTemp(t)
	custom := Input{Name: "C", Source: SourceCustom, CustomScript: "({})", CustomPlaceholders: Placeholders{APIKey: key, AccessToken: token}}
	a, err := s.Create(custom)
	if err != nil {
		t.Fatal(err)
	}
	// To built-in; the form sends the hidden custom fields blank or masked.
	b, err := s.Update(a.ID, Input{Name: "C", Source: SourceNewAPI, BaseURL: "https://x.example.com", Credential: pat,
		CustomPlaceholders: Placeholders{APIKey: Mask(key)}})
	if err != nil || b.Credential != pat || b.CustomPlaceholders.APIKey != "" || b.CustomPlaceholders.AccessToken != "" {
		t.Fatalf("as built-in = %+v %v", b, err)
	}
	raw, _ := os.ReadFile(s.Path())
	if strings.Contains(string(raw), key) || strings.Contains(string(raw), token) {
		t.Fatal("data file still holds the custom secrets of a built-in account")
	}
	// Trying and saving it as custom again with blank secrets uses none.
	custom.CustomPlaceholders = Placeholders{}
	if r, err := Resolve(custom, &b); err != nil || r.CustomPlaceholders.APIKey != "" || r.CustomPlaceholders.AccessToken != "" {
		t.Fatalf("resolve back to custom = %+v %v", r, err)
	}
	c, err := s.Update(a.ID, custom)
	if err != nil || c.CustomPlaceholders.APIKey != "" || c.CustomPlaceholders.AccessToken != "" || c.Credential != "" {
		t.Fatalf("back to custom = %+v %v", c, err)
	}

	// A built-in record written by an older build with leftover custom
	// secrets is read without them.
	leftover := `{"schema_version":1,"accounts":[{"id":"acc_00000000000000b1","order":0,"name":"Old","source":"newapi","base_url":"https://x.example.com","credential":"` + pat + `","proxy":"direct","custom_placeholders":{"apiKey":"` + key + `","accessToken":"` + token + `"}}]}`
	if err := os.WriteFile(s.Path(), []byte(leftover), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := s.Get("acc_00000000000000b1")
	if err != nil || old.CustomPlaceholders.APIKey != "" || old.CustomPlaceholders.AccessToken != "" || old.Credential != pat {
		t.Fatalf("leftover record = %+v %v", old, err)
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

// seedOpen writes a data file holding a credential with the given modes, the
// way a restored backup or a hand-made directory arrives.
func seedOpen(t *testing.T, dir string, dirMode, fileMode os.FileMode) []byte {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema_version":1,"accounts":[{"id":"acc_00000000000000a1","order":0,"name":"Restored","source":"newapi","base_url":"https://site.example.com","credential":"` + pat + `","proxy":"direct"}]}`)
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, fileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	return raw
}

func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// A configured data_dir open to other users is refused, not modified, and
// nothing is read from or written to it until it is made owner-only.
func TestConfiguredDirOpenToOthersIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "restored")
	raw := seedOpen(t, dir, 0o755, 0o644)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	check := func(op string, err error) {
		t.Helper()
		var fe *FileError
		if !errors.As(err, &fe) || fe.Kind != FileUnavailable || fe.Path != dir || fe.Reason() != "dir_not_private" {
			t.Fatalf("%s: err = %v, want data_file_unavailable/dir_not_private at %s", op, err, dir)
		}
	}
	list, err := s.List()
	check("list", err)
	if list != nil {
		t.Fatalf("list served accounts from a refused directory: %+v", list)
	}
	_, err = s.Get("acc_00000000000000a1")
	check("get", err)
	_, err = s.Create(patInput("x"))
	check("create", err)
	_, err = s.Update("acc_00000000000000a1", patInput("x"))
	check("update", err)
	check("delete", s.Delete("acc_00000000000000a1"))
	if d, f := perm(t, dir), perm(t, s.Path()); d != 0o755 || f != 0o644 {
		t.Fatalf("refused directory was modified: dir %#o file %#o", d, f)
	}
	if got, _ := os.ReadFile(s.Path()); string(got) != string(raw) {
		t.Fatalf("refused data file was rewritten: %q", got)
	}

	// Once the user makes the directory owner-only it works again, and the
	// restored file is restricted before it is read.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	list, err = s.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list after chmod 700 = %+v %v", list, err)
	}
	if f := perm(t, s.Path()); f != 0o600 {
		t.Fatalf("file %#o after read, want 0600", f)
	}
}

// A restored data file open to other users in an owner-only directory is
// restricted to 0600 before any credential is read from it.
func TestRestoredFileIsRestrictedBeforeRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "data")
	seedOpen(t, dir, 0o700, 0o644)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Get("acc_00000000000000a1")
	if err != nil || a.Credential != pat {
		t.Fatalf("get = %+v %v", a, err)
	}
	if d, f := perm(t, dir), perm(t, s.Path()); d != 0o700 || f != 0o600 {
		t.Fatalf("dir %#o file %#o, want 0700/0600", d, f)
	}
}

// The default directory belongs to the plugin: it is restricted to 0700 (as
// is a restored file, to 0600) instead of being refused, also when it is
// loosened while CPA runs.
func TestDefaultDirIsKeptOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	seedOpen(t, dir, 0o755, 0o644)
	s, err := OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(); err != nil || len(list) != 1 {
		t.Fatalf("list = %+v %v", list, err)
	}
	if d, f := perm(t, dir), perm(t, s.Path()); d != 0o700 || f != 0o600 {
		t.Fatalf("dir %#o file %#o, want 0700/0600", d, f)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	if d := perm(t, dir); d != 0o700 {
		t.Fatalf("loosened default dir %#o, want 0700", d)
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
