// Package plugin implements the CPA plugin contract (RPC schema 6) in plain
// Go; cmd/newapi-meter only adapts it to the C ABI.
package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/cache"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/customquery"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/newapi"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

// Version is the plugin release version, set at build time with
// -ldflags "-X github.com/qiyi71w/cpa-plugin-newapi-meter/internal/plugin.Version=<v>".
var Version = "0.0.0-dev"

const (
	// ID is the plugin ID CPA derives from the library file name.
	ID = "newapi-meter"
	// SchemaVersion is the RPC schema declared at registration: from 6 on,
	// management JSON responses are not HTML-escaped by the host.
	SchemaVersion = 6
	// MenuName is the host menu label of the page.
	MenuName = "余额用量 · NewAPI Meter"

	managementBase = "/v0/management"
	resourceBase   = "/v0/resource/plugins/" + ID
	// RoutePrefix is the management path prefix of every plugin route.
	RoutePrefix = managementBase + "/plugins/" + ID
	// PagePath is the only resource route.
	PagePath = resourceBase + "/index.html"
)

// Options injects the plugin's environment; zero values use the real ones.
type Options struct {
	// Now returns the current time in CPA's local zone.
	Now func() time.Time
	// HTTP is the outbound entry; the default has no host, so only direct
	// accounts can reach a site.
	HTTP outbound.Doer
	// Log receives plugin logs (text to stderr by default).
	Log io.Writer
	// Page is the embedded HTML page.
	Page []byte
	// DefaultStore opens the data directory used when data_dir is empty.
	DefaultStore func() (*store.Store, error)
}

// Plugin is safe for concurrent calls from the host.
type Plugin struct {
	now          func() time.Time
	http         outbound.Doer
	log          *slog.Logger
	page         []byte
	defaultStore func() (*store.Store, error)
	source       *newapi.Source
	overviews    *cache.Cache[newapi.Reading]
	periods      *cache.Cache[newapi.PeriodSummary]
	custom       *customquery.Runner
	customs      *cache.Cache[customquery.Result]

	mu       sync.RWMutex
	store    *store.Store
	storeErr error
	dataDir  string

	okMu   sync.Mutex
	lastOK map[string]time.Time
}

// New builds a plugin from opts.
func New(opts Options) *Plugin {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HTTP == nil {
		opts.HTTP = outbound.New(nil)
	}
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	if opts.DefaultStore == nil {
		opts.DefaultStore = store.OpenDefault
	}
	p := &Plugin{
		now:          opts.Now,
		http:         opts.HTTP,
		log:          slog.New(slog.NewTextHandler(opts.Log, nil)).With("plugin", ID),
		page:         opts.Page,
		defaultStore: opts.DefaultStore,
		overviews:    cache.New[newapi.Reading](cache.TTL, opts.Now),
		periods:      cache.New[newapi.PeriodSummary](cache.TTL, opts.Now),
		lastOK:       map[string]time.Time{},
		storeErr:     fmt.Errorf("plugin not registered"),
	}
	p.source = &newapi.Source{HTTP: p.http, Now: p.now, UserAgent: "cpa-plugin-newapi-meter/" + Version}
	p.custom = &customquery.Runner{HTTP: p.http, Now: p.now}
	p.customs = cache.New[customquery.Result](cache.TTL, opts.Now)
	return p
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"`
	Error  *envelopeError `json:"error,omitempty"`
}

// Call handles one ABI method and returns the JSON envelope. It never panics.
func (p *Plugin) Call(method string, raw []byte) (out []byte) {
	defer func() {
		if r := recover(); r != nil {
			p.log.Error("plugin call panicked", "method", method)
			out, _ = json.Marshal(envelope{Error: &envelopeError{"internal", "plugin call failed"}})
		}
	}()
	var result any
	switch method {
	case "plugin.register", "plugin.reconfigure":
		result = p.configure(raw)
	case "plugin.quiesce", "plugin.shutdown":
		p.Shutdown()
		result = struct{}{}
	case "management.register":
		result = registration()
	case "management.handle":
		result = p.handle(raw)
	default:
		out, _ = json.Marshal(envelope{Error: &envelopeError{"not_implemented", "unsupported method " + method}})
		return out
	}
	out, err := json.Marshal(envelope{OK: true, Result: result})
	if err != nil {
		out, _ = json.Marshal(envelope{Error: &envelopeError{"internal", "encode result"}})
	}
	return out
}

// Shutdown releases idle connections; the plugin holds no other resources.
func (p *Plugin) Shutdown() {
	if c, ok := p.http.(interface{ CloseIdle() }); ok {
		c.CloseIdle()
	}
}

type configField struct {
	Name        string
	Type        string
	Description string
}

func (p *Plugin) configure(raw []byte) any {
	var req struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	var cfg struct {
		DataDir string `yaml:"data_dir"`
	}
	_ = json.Unmarshal(raw, &req)
	var cfgErr error
	if err := yaml.Unmarshal(req.ConfigYAML, &cfg); err != nil {
		cfgErr = &store.FileError{Kind: store.FileUnavailable, Path: "data_dir", Err: fmt.Errorf("invalid plugin configuration")}
	}
	dir := strings.TrimSpace(cfg.DataDir)

	p.mu.Lock()
	defer p.mu.Unlock()
	if cfgErr == nil && (p.store == nil || dir != p.dataDir) {
		var s *store.Store
		var err error
		if dir == "" {
			s, err = p.defaultStore()
		} else {
			s, err = store.Open(dir)
		}
		p.store, p.storeErr, p.dataDir = s, err, dir
		if err != nil {
			p.store = nil
			p.log.Error("data directory unavailable", "error_kind", kindOf(err))
		}
	} else if cfgErr != nil {
		p.store, p.storeErr = nil, cfgErr
		p.log.Error("plugin configuration invalid")
	}
	return map[string]any{
		"schema_version": SchemaVersion,
		"metadata": map[string]any{
			"Name":             "NewAPI Meter",
			"Version":          Version,
			"Author":           "qiyi71w",
			"GitHubRepository": "https://github.com/qiyi71w/cpa-plugin-newapi-meter",
			"ConfigFields": []configField{{
				Name: "data_dir",
				Type: "string",
				Description: "数据目录（账户数据文件 accounts.json 所在目录，需为绝对路径）；留空时使用系统用户配置目录下的 cpa-plugin-newapi-meter。" +
					" Data directory holding accounts.json (absolute path); empty uses cpa-plugin-newapi-meter under the user config directory.",
			}},
		},
		"capabilities": map[string]bool{"management_api": true},
	}
}

func kindOf(err error) string {
	if fe, ok := err.(*store.FileError); ok {
		return string(fe.Kind)
	}
	return "error"
}

// currentStore returns the configured store or the reason it is unavailable.
func (p *Plugin) currentStore() (*store.Store, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.store, p.storeErr
}

type route struct {
	Method string
	Path   string
}

type resource struct {
	Path        string
	Menu        string
	Description string
}

// Routes is the management route table (method + exact path).
var Routes = []route{
	{"GET", RoutePrefix + "/accounts"},
	{"POST", RoutePrefix + "/accounts"},
	{"PUT", RoutePrefix + "/accounts"},
	{"DELETE", RoutePrefix + "/accounts"},
	{"GET", RoutePrefix + "/overview"},
	{"POST", RoutePrefix + "/accounts/test"},
	{"GET", RoutePrefix + "/detail"},
}

func registration() any {
	return map[string]any{
		"routes": Routes,
		"resources": []resource{{
			Path:        PagePath,
			Menu:        MenuName,
			Description: "账户余额与今日花费 / Account balances and today's spend",
		}},
	}
}
