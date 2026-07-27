package gpac

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Proxy is proxy type defined in pac file
// like
// PROXY 127.0.0.1:8080
// SOCKS 127.0.0.1:1080
type Proxy struct {
	Type     string // Proxy type: PROXY HTTP HTTPS SOCKS DIRECT etc.
	Address  string // Proxy address
	Username string // Proxy username
	Password string // Proxy password

	client *http.Client
	auth   string
	tr     *http.Transport
}

func (p *Proxy) Init() {
	if p.Username != "" && p.Password != "" {
		p.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(p.Username+":"+p.Password))
	}

	p.tr = &http.Transport{
		Proxy: p.Proxy(),
	}

	p.client = &http.Client{
		Transport: p.tr,
	}
}

// IsDirect tests whether it is using direct connection
func (p *Proxy) IsDirect() bool {
	return p.Type == "DIRECT"
}

// IsSOCKS test whether it is a socks proxy
func (p *Proxy) IsSOCKS() bool {
	if len(p.Type) >= 5 {
		return p.Type[:5] == "SOCKS"
	}
	return false
}

// URL returns a url representation for the proxy for curl -x
func (p *Proxy) URL() string {
	switch p.Type {
	case "DIRECT":
		return ""
	case "PROXY":
		return p.Address
	default:
		return fmt.Sprintf("%s://%s", strings.ToLower(p.Type), p.Address)
	}
}

// Proxy returns Proxy function that is ready use for http.Transport
func (p *Proxy) Proxy() func(*http.Request) (*url.URL, error) {
	var u *url.URL
	var ustr string
	var err error

	switch p.Type {
	case "DIRECT":
		break
	case "PROXY":
		if p.Username != "" && p.Password != "" {
			ustr = fmt.Sprintf("http://%s:%s@%s", p.Username, p.Password, p.Address)
		} else {
			ustr = fmt.Sprintf("http://%s", p.Address)
		}
	default:
		if p.Username != "" && p.Password != "" {
			ustr = fmt.Sprintf("%s:%s@%s://%s", p.Username, p.Password, strings.ToLower(p.Type), p.Address)
		} else {
			ustr = fmt.Sprintf("%s://%s", strings.ToLower(p.Type), p.Address)
		}
	}

	if ustr != "" {
		u, err = url.Parse(ustr)
	}

	return func(*http.Request) (*url.URL, error) {
		return u, err
	}
}

var zeroDialer net.Dialer

// Dialer returns a Dial function that will connect to remote address
func (p *Proxy) Dialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	switch p.Type {
	case "DIRECT":
		return (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext
	case "SOCKS", "SOCKS5":
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			d := socksNewDialer(network, p.Address)
			conn, err := d.DialContext(ctx, network, address)
			return conn, err
		}
	case "PROXY", "HTTP":
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := zeroDialer.DialContext(ctx, network, p.Address)

			header := make(http.Header)
			if p.auth != "" {
				header.Add("Authorization", p.auth)
			}

			//log.Printf("Dial %s [address:%s] [p.address: %s] header: %v", network, address, p.Address, header)

			if err == nil {
				connectReq := &http.Request{
					Method: "CONNECT",
					URL:    &url.URL{Opaque: address},
					Host:   address,
					Header: header,
				}
				connectReq.Write(conn)
			}
			return conn, err
		}
	default:
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("%s not support", p.Type)
		}
	}
}

// Client returns an http.Client ready for use with this proxy
func (p *Proxy) Client() *http.Client {
	return p.client
}

// Get issues a GET to the specified URL via the proxy
func (p *Proxy) Get(urlstr string) (*http.Response, error) {
	return p.Client().Get(urlstr)
}

// Transport get the http.RoundTripper
func (p *Proxy) Transport() *http.Transport {
	return p.tr
}

// Do sends an HTTP request via the proxy and returns an HTTP response
func (p *Proxy) Do(req *http.Request) (*http.Response, error) {
	return p.Client().Do(req)
}

func (p *Proxy) String() string {
	if p.IsDirect() {
		return p.Type
	}
	return fmt.Sprintf("%s %s", p.Type, p.Address)
}

func split(source string, s rune) []string {
	return strings.FieldsFunc(source, func(r rune) bool {
		return r == s
	})
}

// ParseProxy parses proxy string returned by FindProxyForURL
// and returns a slice of proxies
func ParseProxy(pstr string) []*Proxy {
	var proxies []*Proxy
	for _, p := range split(pstr, ';') {
		typeAddr := strings.Fields(p)
		if len(typeAddr) == 2 {
			typ := strings.ToUpper(typeAddr[0])
			addr := typeAddr[1]
			var user, pass string
			if at := strings.Index(addr, "@"); at > 0 {
				auth := split(addr[:at], ':')
				if len(auth) == 2 {
					user = auth[0]
					pass = auth[1]
				}
				addr = addr[at+1:]
			}
			proxy := &Proxy{
				Type:     typ,
				Address:  addr,
				Username: user,
				Password: pass,
			}
			proxy.Init()
			proxies = append(proxies, proxy)
		} else if len(typeAddr) == 1 {
			proxies = append(proxies,
				&Proxy{
					Type: strings.ToUpper(typeAddr[0]),
				},
			)
		}
	}

	return proxies
}
