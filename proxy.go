package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// Interface representing an abstract proxy object. Implementations for HTTP CONNECT and SOCKS5 are defined in httpconnect.go and socks5.go.
// Support for other proxy types can be added by defining types implementing the proxy interface.
type proxy interface {
	// handshake takes net.Conn (representing a TCP socket) and an address and returns the same net.Conn connected to the provided address through the proxy
	handshake(net.Conn, string) error
	// address returns the address where the proxy is exposed, i.e. proxy.host:proxy.port
	address() string
}

type baseProxy struct {
	prot string
	host string
	port string
	user string
	pass string
}

type proxyMap map[string]proxy

func (p *baseProxy) UnmarshalJSON(b []byte) error {
	type tmpBaseProxy struct {
		ConnString string
		User       string
		Pass       string
	}

	var tmp tmpBaseProxy

	err := json.Unmarshal(b, &tmp)
	if err != nil {
		err = fmt.Errorf("error unmarshalling '%s' in tmpBaseProxy : %v", b, err)
		return err
	}

	tmp2, err := newBaseProxyFromString(tmp.ConnString, tmp.User, tmp.Pass)
	if err != nil {
		err = fmt.Errorf("error creating new server from string: %v", err)
		return err
	}

	p.prot = tmp2.prot
	p.host = tmp2.host
	p.port = tmp2.port
	p.user = tmp2.user
	p.pass = tmp2.pass

	return nil
}

func (p *proxyMap) UnmarshalJSON(b []byte) error {
	var tmp map[string]baseProxy

	err := json.Unmarshal(b, &tmp)
	if err != nil {
		err = fmt.Errorf("error unmarshalling '%s' in map[string]baseProxy : %v", b, err)
		return err
	}
	*p = make(map[string]proxy)
	gMetaLogger.Debug("ok")
	for k, v := range tmp {
		(*p)[k], err = newProxy(v.prot, v.host, v.port, v.user, v.pass)
		if err != nil {
			err = fmt.Errorf("error creating new proxy from baseProxy %v", v)
			return err
		}
	}

	return nil
}

func newBaseProxyFromString(connString string, user string, pass string) (*baseProxy, error) {
	gMetaLogger.Debugf("Entering newBaseProxyFromString()")
	defer gMetaLogger.Debugf("Leaving newBaseProxyFromString()")

	s1 := strings.Split(connString, "://")
	if len(s1) != 2 {
		return nil, fmt.Errorf("wrong connection string format")
	}
	prot := s1[0]
	s2 := s1[1]

	s3 := strings.Split(s2, ":")
	if len(s3) != 2 {
		return nil, fmt.Errorf("wrong connection string format")
	}

	host := s3[0]
	port := s3[1]

	return &baseProxy{prot: prot, host: host, port: port, user: user, pass: pass}, nil
}

func newProxy(prot string, host string, port string, user string, pass string) (proxy, error) {
	switch prot {
	case "socks5":
		return socks5{baseProxy{prot: prot, host: host, port: port, user: user, pass: pass}}, nil
	case "httpconnect", "http":
		return httpConnect{baseProxy{prot: prot, host: host, port: port, user: user, pass: pass}}, nil
	default:
		err := fmt.Errorf("unknown proxy protocol %v", prot)
		return nil, err
	}
}

// A proxyChain struct represents a chain of proxy interfaces stored in proxies, and some parameters associated to the chain.
// The parameters correspond to the proxychains-ng configuration file parameters (https://github.com/rofl0r/proxychains-ng).

type proxyChain struct {
	proxyDns          bool  // if false, hostnames are resolved locally and IP addresses are used in proxies' handshakes. If true, hostnames are passed to proxies as is.
	tcpConnectTimeout int64 // not used for now. TODO: implement it
	tcpReadTimeout    int64
	proxies           []proxy // ordered list of proxies to connect through
}

type proxyChainDesc struct {
	ProxyDns          bool
	TcpConnectTimeout int64
	TcpReadTimeout    int64
	Proxies           []string
}

func (p *proxyChainDesc) UnmarshalJSON(b []byte) error {
	type defaults proxyChainDesc

	tmp := defaults{ProxyDns: true, TcpConnectTimeout: 1000, TcpReadTimeout: 2000}

	err := json.Unmarshal(b, &tmp)
	if err != nil {
		err = fmt.Errorf("error unmarshalling '%s' in proxyChainDesc : %v", b, err)
		return err
	}
	*p = proxyChainDesc(tmp)

	return nil
}

type chainMap map[string]proxyChainDesc

// connect takes a destination address string (format host:port) and returns a net.Conn connected to this address through the chain of proxies.
func (chain proxyChain) connect(ctx context.Context, address string) (net.Conn, string, error) {

	// If custom hosts are provided in the hosts section of the configuration, the matching hostnames are replaced by their hardcoded IP address.
	// This overrides proxyDns: matching hostnames will be replaces by their IP address even if proxyDns=true.
	if len(gHosts) != 0 {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			werr := fmt.Errorf("could not split host from %v : %w", address, err)
			return nil, "", werr
		}

		resolved, ok := gHosts[host]
		if ok {
			gMetaLogger.Debugf("%v appears in custom hosts file, resolving it to %v", host, resolved)
			address = net.JoinHostPort(resolved, port)
		}
	}

	// If proxyDns=false, perform local DNS resolution of hostnames contained in address
	// DNS resolution step is not accounted for in timeouts.
	if !chain.proxyDns {

		host, port, err := net.SplitHostPort(address) // splits the provided address string (host:port format) into a host and a port string
		if err != nil {
			werr := fmt.Errorf("could not split host from %v : %w", address, err)
			return nil, "", werr
		}

		if net.ParseIP(host) == nil { // host does not have an IP address format
			gMetaLogger.Debugf("Chain is configured with proxyDns=false. Performing local DNS resolution of %v", host)
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				werr := fmt.Errorf("lookup on %v failed: %w", host, err)
				return nil, "", werr
			}

			if len(ips) == 0 {
				err := fmt.Errorf("no IP returned from DNS resolution of %v", host)
				return nil, "", err
			}

			gMetaLogger.Debugf("Found IP address: %v", ips[0])
			address = net.JoinHostPort(ips[0].String(), port) // use the first IP address returned instead of the hostname in address
		}

	}
	gMetaLogger.Debugf("Initiate connection to %v", address)

	// timeout context used to stop the connection through the proxy chain after chain.tcpReadTimeout millisecond
	gMetaLogger.Debugf("timeout : %v", chain.tcpReadTimeout)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(chain.tcpReadTimeout)*time.Millisecond)
	defer cancel()

	// Start connectN
	conn, repr, err := chain.connectN(ctx, len(chain.proxies), address)
	gMetaLogger.Debugf("connectN returned before timeout")
	return conn, repr, err

}

// connectN is a recursive function returning a net.Conn (representing a TCP socket) connected to address through the subchain made of the n first proxies of the proxy chain.
// It takes ctx context parameter for timeout implementation.
func (chain proxyChain) connectN(ctx context.Context, n int, address string) (conn net.Conn, repr string, err error) {
	var d net.Dialer

	repr = ""

	if n == 0 { // If the subchain contains no proxy, directly connect to the provided address
		gMetaLogger.Debugf("connectN called with n=0. Connect to %v directly.", address)
		conn, err = d.DialContext(ctx, "tcp", address)
		if err != nil {
			repr += fmt.Sprintf("-X-> %v (%v)", address, err.Error())
		} else {
			repr += fmt.Sprintf("---> %v", address)
		}
		return
	} else { // Otherwise, connect recursively through the whole subchain

		if n == 1 { // If the subchain contains only one proxy, establish a direct TCP connection to the proxy and obtain net.Conn with net.Dial
			gMetaLogger.Debugf("connectN called with n=1. Connect to the only proxy %v", (chain.proxies[n-1]).address())
			conn, err = d.DialContext(ctx, "tcp", (chain.proxies[n-1]).address())
			if err != nil {
				repr += fmt.Sprintf("-X-> %v (%v)", (chain.proxies[n-1]).address(), err.Error())
				return
			}
			repr += fmt.Sprintf("---> %v", (chain.proxies[n-1]).address())

		} else { // Otherwise (multiple proxies), recursively call connectN to obtain an "indirect" TCP connection to the suchain's last proxy through the 1-proxy-shorter subchain.
			gMetaLogger.Debugf("connectN called with n=%v (>1). Recursively calling connectN.", n)

			conn, repr, err = chain.connectN(ctx, n-1, (chain.proxies[n-1]).address())
			if err != nil {
				return
			}
		}

		// Once we have a connection to the subchain's last proxy, proceed to the subchain's last proxy's handshake to connect to provided address
		// TODO: implement a timeout on the handshake
		gMetaLogger.Debugf("Establishing connection to %v through proxy %v", address, (chain.proxies[n-1]).address())
		resultCh := make(chan error)

		go func() {
			resultCh <- (chain.proxies[n-1]).handshake(conn, address)
			close(resultCh)
		}()

		select {
		case result := <-resultCh:
			gMetaLogger.Debugf("handshake returned before timeout")
			err = result
		case <-ctx.Done():
			gMetaLogger.Errorf("timeout during handshake with %v for %v", chain.proxies[n-1].address(), address)
			err = fmt.Errorf("timeout during handshake()")
		}

		if err != nil {
			conn.Close() // Should cancel any read or write operation on conn in handshake() in case ctx is Done
			conn = nil
			repr += fmt.Sprintf(" =X=> %v (%v)", address, err.Error())
			return
		}
		repr += fmt.Sprintf(" ===> %v", address)
	}

	return
}
