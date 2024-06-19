package main

// Defines functions to run the input servers (SOCKS5 and HTTP CONNECT) and to handle incomming client connections.

import (
	"context"
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
	connHandle(client net.Conn, table string, ctx context.Context, cancel context.CancelFunc)
}

type server struct {
	prot    string
	addr    string
	port    string
	table   string
	handler connHandler
}

func newServer(srvString string) (*server, error) {
	gMetaLogger.Debugf("Entering newServer()")
	defer gMetaLogger.Debugf("Leaving newServer()")

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

	s := new(server)

	s.addr = addr
	s.port = port
	s.table = table

	switch prot {
	case "socks5":
		s.prot = prot
		s.handler = new(socks5Handler)
	case "http":
		s.prot = prot
		s.handler = new(httpHandler)
	default:
		return nil, fmt.Errorf("%v handler type does not exist", prot)
	}

	return s, nil
}

func (s server) address() string {
	return fmt.Sprintf("%s:%s", s.addr, s.port)
}

func (s server) String() string {
	return fmt.Sprintf("%s://%s:%s:%s", s.prot, s.addr, s.port, s.table)
}

// run runs an input server of type serverType listening on address
func (s server) run() {

	// Creates a TCP socket and listen on address for incomming client connections
	l, err := net.Listen("tcp", s.address())
	if err != nil {
		gMetaLogger.Panic(err)
	}
	gMetaLogger.Infof("connHandler started on %v", s.address())

	// For each client connection received on the listening socket, create a context and start a goroutine handling the connection
	for {
		var c net.Conn
		c, err = l.Accept()
		if err != nil {
			gMetaLogger.Error(err)
			continue
		}
		gMetaLogger.Debugf("new connection (%v) accepted", c)

		ctx, cancel := context.WithCancel(context.Background())

		go s.handler.connHandle(c, s.table, ctx, cancel)

	}
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

	gMetaLogger.Debug("Waiting for both goroutines to complete")
	wg.Wait()
	gMetaLogger.Debug("Goroutines ended")

}
