package main

// Defines functions to run the input servers (SOCKS5 and HTTP CONNECT) and to handle incomming client connections.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

const (
	atypIPV4   byte = 1 // SOCKS5 IP V4 address address type (see RFC 1928)
	atypDomain byte = 3 // SOCKS5 DOMAINNAME address type (see RFC 1928)
	atypIPV6   byte = 4 // SOCKS5 IP V6 address address type (see RFC 1928)

	cmdConnect      byte = 1 // SOCKS5 request CONNECT command (see RFC 1928)
	cmdBind         byte = 2 // SOCKS5 request BIND command (see RFC 1928)
	cmdUDPAssociate byte = 3 // SOCKS5 request UDP ASSOCIATE command (see RFC 1928)
)

type connHandler interface {
	connHandle(client net.Conn, ctx context.Context, cancel context.CancelFunc)
}

type server struct {
	descrString string // We keep this even if it is redundant to ease comparison of servers
	prot        string
	addr        string
	port        string
	table       string
	handler     connHandler
	ctx         context.Context
	cancel      context.CancelFunc
	running     bool
}

// serverConf is the type used to hold and access a server configuration (defined in a file)
type serverConf struct {
	servers []server
	valid   bool // whether the current configuration is valid
	mu      sync.RWMutex
}

func newServer(descr string, prot string, addr string, port string, table string, dest string, chain string) (*server, error) {
	gMetaLogger.Debugf("Entering newServer()")
	defer gMetaLogger.Debugf("Leaving newServer()")

	var handler connHandler

	switch prot {
	case "socks5":
		if table == "" {
			return nil, fmt.Errorf("table cannot be empty for a socks5 server")
		}
		handler = &socks5Handler{table: table}
	case "http":
		if table == "" {
			return nil, fmt.Errorf("table cannot be empty for a http server")
		}
		handler = &httpHandler{table: table}
	case "fwd":
		if dest == "" || chain == "" {
			return nil, fmt.Errorf("dest and chain cannot be empty for a forward server")
		}
		handler = &forwardHandler{dest: dest, chain: chain}

	default:
		return nil, fmt.Errorf("%v handler type does not exist", prot)
	}

	s := &server{
		descrString: descr,
		prot:        prot,
		addr:        addr,
		port:        port,
		table:       table,
		handler:     handler,
		ctx:         nil,
		cancel:      nil,
		running:     false,
	}
	return s, nil
}

func newServerFromString(srvString string) (*server, error) {
	gMetaLogger.Debugf("Entering newServerFromString()")
	defer gMetaLogger.Debugf("Leaving newServerFromString()")

	s1 := strings.Split(srvString, "://")
	if len(s1) != 2 {
		return nil, fmt.Errorf("wrong server string format")
	}
	prot := s1[0]
	s2 := s1[1]

	s3 := strings.Split(s2, ":")
	if len(s3) != 3 && len(s3) != 5 {
		return nil, fmt.Errorf("wrong server string format")
	}

	addr := s3[0]
	port := s3[1]
	table := ""
	dest := ""
	chain := ""
	if prot == "socks5" || prot == "http" {
		table = s3[2]
	} else if prot == "fwd" {
		chain = s3[2]
		dest = strings.Join(s3[3:5], ":")
	}

	return newServer(srvString, prot, addr, port, table, dest, chain)
}

// Custom JSON unmarshaller describing how to parse a server type from a string like "socsk5://127.0.0.1:1337:table1"
func (server *server) UnmarshalJSON(b []byte) error {

	var serverString string

	err := json.Unmarshal(b, &serverString)
	if err != nil {
		err = fmt.Errorf("error unmarshalling '%s' in string : %v", b, err)
		return err
	}

	tmpServer, err := newServerFromString(serverString)
	if err != nil {
		err = fmt.Errorf("error creating new server from string: %v", err)
		return err
	}

	server.descrString = tmpServer.descrString
	server.addr = tmpServer.addr
	server.port = tmpServer.port
	server.prot = tmpServer.prot
	server.table = tmpServer.table
	server.ctx = tmpServer.ctx
	server.cancel = tmpServer.cancel
	server.handler = tmpServer.handler

	return nil
}

func (s server) address() string {
	return fmt.Sprintf("%s:%s", s.addr, s.port)
}

func (s server) String() string {
	return fmt.Sprintf("%s[running:%v, handler:%v]", s.descrString, s.running, s.handler)
}

// run runs an input server of type serverType listening on address. It returns if and only if the
// context it creates is cancelled (i.e. the server's stop() method is called)
func (s *server) run() {
	gMetaLogger.Debugf("Entering %v(%p).run()", s, s)
	defer gMetaLogger.Debugf("Leaving %v(%p).run()", s, s)

	// Create a new context and store it in the server struct
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = serverCtx
	s.cancel = cancel
	s.running = true

	// Creates a TCP socket and listen on address for incomming client connections
	l, err := net.Listen("tcp", s.address())
	if err != nil {
		gMetaLogger.Panic(err)
	}
	defer l.Close()
	gMetaLogger.Infof("%v server started on %v", s.prot, s.address())

	// For each client connection received on the listening socket, create a context and start a goroutine handling the connection
	for {
		acceptDone := make(chan struct{})
		var c net.Conn
		gMetaLogger.Debugf("c[%%p]: %p", c)
		go func() { // The only blocking part of this goroutine is l.Accept(), which is unblocked if l is closed, which happens when we return after s.ctx is canceled
			c, err = l.Accept()
			if err != nil {
				gMetaLogger.Error(err)
				close(acceptDone)
				return
			}
			gMetaLogger.Debugf("New net.Conn accepted (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", c, c, &c)

			connCtx, connCancel := context.WithCancel(s.ctx)
			go s.handler.connHandle(c, connCtx, connCancel)

			close(acceptDone)
		}()

		select {
		case <-s.ctx.Done():
			gMetaLogger.Debugf("Context of server %v has been canceled, returning from run()", s)
			return //causes l to be closed (see defer upper) and thus the last running Accept goroutine to return.
		case <-acceptDone: //if we arrive here, the previous anonymous goroutine has returned, c may be nil or a connected client
			// being handled in the connHandle goroutine. c will be closed when run() returns (ie. when s.stop() is called), or
			// by connHandle() when it returns
			gMetaLogger.Debugf("c[%%p]: %p", c)
			defer c.Close()
			continue
		}
	}
}

func (s *server) stop() {
	gMetaLogger.Debugf("Entering %v.stop()", s)
	defer gMetaLogger.Debugf("Leaving %v.stop()", s)

	if s.running {
		gMetaLogger.Debugf("%v server is running, stopping it.", s)
		s.cancel()
		s.running = false
	}
}

func compare(s1 server, s2 server) (equal bool) {
	return s1.descrString == s2.descrString
}

// relay takes two net.Conn target and client (representing TCP sockets) and transfers data between them.
func relay(client net.Conn, target net.Conn) {

	var wg sync.WaitGroup

	wg.Add(1)
	// Transfer from target to client
	go func() {
		defer wg.Done()
		defer client.Close()
		defer target.Close()

		written, err := io.Copy(client, target)

		gMetaLogger.Debugf("%v bytes sent from target %v to client %v", written, target, client)
		if err != nil {
			gMetaLogger.Debugf("copy from target to client returned an error: %v", err)
		}
	}()

	wg.Add(1)
	// Transfer from client to target
	go func() {
		defer wg.Done()
		defer client.Close()
		defer target.Close()

		written, err := io.Copy(target, client)

		gMetaLogger.Debugf("%v bytes sent from client %v to target %v", written, client, target)
		if err != nil {
			gMetaLogger.Debugf("copy from client to target returned an error: %v", err)
		}
	}()

	gMetaLogger.Debug("Waiting for both relay goroutines to complete")
	wg.Wait()
	gMetaLogger.Debug("Relay goroutines ended")

}

func describeServers(servers []server) {
	gMetaLogger.Debugf("Describing server slice %p : %v", servers, servers)
	for i := 0; i < len(servers); i++ {
		gMetaLogger.Debugf("Index %v. Server %p : %v", i, &(servers[i]), servers[i])
	}
}
