package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imgk/shadow/pkg/logger"
	"github.com/imgk/shadow/pkg/suffixtree"
)

// Conf is shadow application configuration
type Conf struct {
	// Server Config
	Server      string `json:"server"`
	NameServer  string `json:"name_server"`
	ProxyServer string `json:"proxy_server,omitempty"`

	// WinDivert
	FilterString string `json:"windivert_filter_string,omitempty"`
	GeoIP        struct {
		File   string   `json:"file"`
		Proxy  []string `json:"proxy,omitempty"`
		Bypass []string `json:"bypass,omitempty"`
		Final  string   `json:"final"`
	} `json:"geo_ip_rules,omitempty"`
	AppRules struct {
		Proxy []string `json:"proxy"`
	} `json:"app_rules,omitempty"`

	// Tun
	TunName string   `json:"tun_name,omitempty"`
	TunAddr []string `json:"tun_addr,omitempty"`

	// Tun and WinDivert
	IPCIDRRules struct {
		Proxy []string `json:"proxy"`
	} `json:"ip_cidr_rules"`
	DomainRules struct {
		Proxy   []string `json:"proxy"`
		Direct  []string `json:"direct,omitempty"`
		Blocked []string `json:"blocked,omitempty"`
	} `json:"domain_rules"`
}

// ReadFromFile is to read config from file
func (c *Conf) ReadFromFile(file string) error {
	file, err := filepath.Abs(file)
	if err != nil {
		return err
	}

	info, err := os.Stat(file)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return errors.New("not a file")
	}

	b, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	return c.ReadFromByteSlice(b)
}

// ReadFromByteSlice is to load config from byte slice
func (c *Conf) ReadFromByteSlice(b []byte) error {
	if err := json.Unmarshal(b, c); err != nil {
		return err
	}
	for _, v := range c.GeoIP.Proxy {
		c.GeoIP.Proxy = append(c.GeoIP.Proxy, strings.ToUpper(v))
	}
	for _, v := range c.GeoIP.Bypass {
		c.GeoIP.Bypass = append(c.GeoIP.Bypass, strings.ToUpper(v))
	}
	c.GeoIP.Final = strings.ToLower(c.GeoIP.Final)
	return nil
}

// App is shadow application
type App struct {
	Logger logger.Logger
	Conf   *Conf

	timeout time.Duration

	closed  chan struct{}
	closers []io.Closer
}

// NewApp is new shadow app from config file
func NewApp(file string, timeout time.Duration, w io.Writer) (*App, error) {
	conf := new(Conf)
	if err := conf.ReadFromFile(file); err != nil {
		return nil, err
	}

	return NewAppFromConf(conf, timeout, w), nil
}

// NewAppFromByteSlice is new shadow app from byte slice
func NewAppFromByteSlice(b []byte, timeout time.Duration, w io.Writer) (*App, error) {
	conf := new(Conf)
	if err := conf.ReadFromByteSlice(b); err != nil {
		return nil, err
	}

	return NewAppFromConf(conf, timeout, w), nil
}

// NewAppFromConf is new shadow app from *Conf
func NewAppFromConf(conf *Conf, timeout time.Duration, w io.Writer) *App {
	app := &App{
		Logger:  logger.NewLogger(w),
		Conf:    conf,
		timeout: timeout,
		closed:  make(chan struct{}),
		closers: []io.Closer{},
	}
	return app
}

func (app *App) attachCloser(closer io.Closer) {
	app.closers = append(app.closers, closer)
}

// Done is to give done channel
func (app *App) Done() chan struct{} {
	return app.closed
}

// Close is shutdown application
func (app *App) Close() error {
	select {
	case <-app.closed:
		return nil
	default:
	}
	for _, closer := range app.closers {
		closer.Close()
	}
	close(app.closed)
	return nil
}

// NewDomainTree is ...
func NewDomainTree(app *App) (*suffixtree.DomainTree, error) {
	tree := suffixtree.NewDomainTree(".")
	tree.Lock()
	for _, domain := range app.Conf.DomainRules.Proxy {
		tree.UnsafeStore(domain, &suffixtree.DomainEntry{Rule: "PROXY"})
	}
	for _, domain := range app.Conf.DomainRules.Direct {
		tree.UnsafeStore(domain, &suffixtree.DomainEntry{Rule: "DIRECT"})
	}
	for _, domain := range app.Conf.DomainRules.Blocked {
		tree.UnsafeStore(domain, &suffixtree.DomainEntry{Rule: "BLOCKED"})
	}
	tree.Unlock()
	return tree, nil
}

// ServePAC is to serve proxy pac file
func ServePAC(w http.ResponseWriter, r *http.Request) {
	const Format = `function FindProxyForURL(url, host) {
	if (isInNet(dnsResolve(host), "198.18.0.0", "255.255.0.0")) {
		return "SOCKS5 %s"
	}
	return "DIRECT"
}
`

	w.Header().Add("Content-Type", "application/x-ns-proxy-autoconfig")
	fmt.Fprintf(w, Format, r.Host)
}
