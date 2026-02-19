package gpac

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// Parser the parsed pac instance
type Parser struct {
	vm  *goja.Runtime
	src string // the FindProxyForURL source code

	sync.Mutex
}

// FindProxyForURL finding proxy for url
// returns string like:
// PROXY 4.5.6.7:8080; PROXY 7.8.9.10:8080; DIRECT
func (p *Parser) FindProxyForURL(urlstr string) (string, error) {
	u, err := url.Parse(urlstr)
	if err != nil {
		return "", err
	}

	f := fmt.Sprintf("FindProxyForURL('%s', '%s')", urlstr, u.Hostname())
	p.Lock()
	r, err := p.vm.RunString(f)
	p.Unlock()

	if err != nil {
		return "", err
	}
	return r.String(), nil
}

// FindProxy find the proxy in pac and return a list of Proxy
func (p *Parser) FindProxy(urlstr string) ([]*Proxy, error) {
	ps, err := p.FindProxyForURL(urlstr)
	if err != nil {
		return nil, err
	}

	return ParseProxy(ps), nil
}

// Get issues a GET to the specified URL via the proxy list found
// it stops at the first proxy that succeeds
func (p *Parser) Get(urlstr string) (*http.Response, error) {
	req, err := http.NewRequest("GET", urlstr, nil)
	if err != nil {
		return nil, err
	}

	return p.Do(req)
}

// Do sends an HTTP request via a list of proxies found
// it returns first HTTP response that succeeds
func (p *Parser) Do(req *http.Request) (*http.Response, error) {
	ps, err := p.FindProxyForURL(req.URL.String())
	if err != nil {
		return nil, err
	}

	proxies := ParseProxy(ps)
	if len(proxies) == 0 {
		return nil, errors.New("no proxies found")
	}

	for _, proxy := range proxies {
		resp, err := proxy.Do(req)
		if err == nil {
			return resp, nil
		}
	}
	return nil, errors.New("no request via proxies succeeds")
}

// Source returns the original javascript snippet of the pac
func (p *Parser) Source() string {
	return p.src
}

// New create a parser from text content
func New(text string) (*Parser, error) {
	vm := goja.New()
	registerBuiltinNatives(vm)
	registerBuiltinJS(vm)

	_, err := vm.RunString(text)
	if err != nil {
		return nil, err
	}

	return &Parser{vm: vm, src: text}, nil
}

func registerBuiltinJS(vm *goja.Runtime) {
	_, err := vm.RunString(builtinJS)
	if err != nil {
		panic(err)
	}
}

func registerBuiltinNatives(vm *goja.Runtime) {
	for name, function := range builtinNatives {
		vm.Set(name, function(vm))
	}
}

func fromReader(r io.ReadCloser) (*Parser, error) {
	defer r.Close()
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return New(string(buf))
}

// FromFile load pac from file
func FromFile(filename string) (*Parser, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return fromReader(f)
}

// FromURL load pac from url
func FromURL(urlstr string) (*Parser, error) {
	resp, err := http.Get(urlstr)
	if err != nil {
		return nil, err
	}
	return fromReader(resp.Body)
}

// From load pac from file or url
func From(dst string) (*Parser, error) {
	if strings.HasPrefix(dst, "http://") ||
		strings.HasPrefix(dst, "https://") {
		return FromURL(dst)
	}

	return FromFile(dst)
}
