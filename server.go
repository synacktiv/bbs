package main

// Defines functions to run the input servers (SOCKS5 and HTTP CONNECT) and to handle incomming client connections.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
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
	connHandle(client net.Conn, table string, ctx context.Context, cancel context.CancelFunc)
}

type server struct {
	prot    string
	addr    string
	port    string
	table   string
	handler connHandler
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
}

// serverConf is the type used to hold and access a server configuration (defined in a file)
type serverConf struct {
	servers []server
	valid   bool // whether the current configuration is valid
	mu      sync.RWMutex
}

func newServer(prot string, addr string, port string, table string) (*server, error) {
	gMetaLogger.Debugf("Entering newServer()")
	defer gMetaLogger.Debugf("Leaving newServer()")

	var handler connHandler

	switch prot {
	case "socks5":
		handler = new(socks5Handler)
	case "http":
		handler = new(httpHandler)
	default:
		return nil, fmt.Errorf("%v handler type does not exist", prot)
	}

	s := &server{
		prot:    prot,
		addr:    addr,
		port:    port,
		table:   table,
		handler: handler,
		ctx:     nil,
		cancel:  nil,
		running: false,
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
	if len(s3) != 3 {
		return nil, fmt.Errorf("wrong server string format")
	}

	addr := s3[0]
	port := s3[1]
	table := s3[2]

	return newServer(prot, addr, port, table)
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

	server.addr = tmpServer.addr
	server.port = tmpServer.port
	server.prot = tmpServer.prot
	server.table = tmpServer.table
	server.ctx = tmpServer.ctx
	server.cancel = tmpServer.cancel
	server.handler = tmpServer.handler

	return nil
}

// parseServerConfig parses the JSON servers configuration file at configPath and returns a []server variable representing this configuration.
// The config file should be a JSON like :
// [
//
//	"socks5://127.0.0.1:1080:table1",
//	"http://127.0.0.1:8080:table2"
//
// ]
// If PAC is used, the table is ignored and can be set to "pac"
func parseServerConfig(configPath string) ([]server, error) {

	fileBytes, err := os.ReadFile(configPath)
	if err != nil {
		err := fmt.Errorf("error reading file %v : %v", configPath, err)
		return nil, err
	}

	var servers []server

	dec := json.NewDecoder(bytes.NewReader(fileBytes))
	dec.DisallowUnknownFields()

	err = dec.Decode(&servers)
	if err != nil {
		err = fmt.Errorf("error unmarshalling server config file : %v", err)
		return nil, err
	}

	return servers, nil
}

func (s server) address() string {
	return fmt.Sprintf("%s:%s", s.addr, s.port)
}

func (s server) String() string {
	return fmt.Sprintf("%s://%s:%s:%s[running:%v, handler:%v]", s.prot, s.addr, s.port, s.table, s.running, s.handler)
}

// run runs an input server of type serverType listening on address
func (s *server) run() {
	gMetaLogger.Debugf("Entering %v(%p).run()", s, s)
	defer gMetaLogger.Debugf("Leaving %v(%p).run()", s, s)

	// Create a new context and store it in the server struct
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx
	s.cancel = cancel
	s.running = true

	// Creates a TCP socket and listen on address for incomming client connections
	l, err := net.Listen("tcp", s.address())
	if err != nil {
		gMetaLogger.Panic(err)
	}
	defer l.Close()
	gMetaLogger.Infof("connHandler started on %v", s.address())

	// For each client connection received on the listening socket, create a context and start a goroutine handling the connection
	for {
		acceptDone := make(chan struct{})
		go func() {
			var c net.Conn
			c, err = l.Accept()
			if err != nil {
				gMetaLogger.Error(err)
				close(acceptDone)
				return
			}
			gMetaLogger.Debugf("new connection (%v) accepted", c)

			ctx, cancel := context.WithCancel(s.ctx)

			go s.handler.connHandle(c, s.table, ctx, cancel)
			close(acceptDone)
		}()

		select {
		case <-s.ctx.Done():
			return //causes l to be closed (see defer upper) and thus the last running Accept goroutine to return.
		case <-acceptDone:
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
	equal = ((s1.addr == s2.addr) && (s1.port == s2.port) && (s1.prot == s2.prot) && (s1.table == s2.table))
	return
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
