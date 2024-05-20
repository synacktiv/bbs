package main

// Defines functions to run the input servers (SOCKS5 and HTTP CONNECT) and to handle incomming client connections.

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
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

type ServerType byte

const (
	HTTPCONNECT ServerType = iota // HTTP CONNECT input server type
	SOCKS5                        // SOCKS5 input server type
)

// run runs an input server of type serverType listening on address
func run(address string, serverType ServerType) {

	// Creates a TCP socket and listen on address for incomming client connections
	l, err := net.Listen("tcp", address)
	if err != nil {
		gMetaLogger.Panic(err)
	}
	gMetaLogger.Infof("Listener started on %v", address)

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

		switch serverType {
		case HTTPCONNECT:
			go handleHttpCONNECTConnection(c, ctx, cancel)
		case SOCKS5:
			go handleSOCKSConnection(c, ctx, cancel)
		}

	}
}

// handleSOCKSConnection handles the connection of a client on the input SOCKS5 listener.
// It parses the SOCKS command, establishes a connection to the requested host through the right chain,
// transfers data between the established connecion socket and the clien socket, and finally closes evetything on errors or at the end.
func handleSOCKSConnection(client net.Conn, ctx context.Context, cancel context.CancelFunc) {
	gMetaLogger.Debugf("Entering handleConnection for connection %v", &client)
	defer func() { gMetaLogger.Debugf("Leavings handleConnection for connection %v", &client) }()

	defer client.Close()

	// ***** BEGIN SOCKS5 input parsing *****

	// Parse SOCKS5 input to retrieve command, target host and target port (see RFC 1928)

	reader := bufio.NewReader(client)

	// Read version and number of methods
	buff := make([]byte, 2)
	_, err := io.ReadFull(reader, buff)
	if err != nil {
		gMetaLogger.Errorf("could not read on client socket: %v", err)
		return
	}

	if buff[0] != 5 {
		gMetaLogger.Error("only SOCKS5 is supported")
		return
	}

	gMetaLogger.Debugf("received SOCKS %v connection with %v proposed methods", buff[0], buff[1])

	// Read methods
	buff = make([]byte, buff[1])
	_, err = io.ReadFull(reader, buff)
	if err != nil {
		gMetaLogger.Errorf("could not read on client socket: %v", err)
		return
	}
	gMetaLogger.Debugf("Following methods are proposed: %v", buff)

	method := byte(255)
	for _, m := range buff {
		if m == 0 {
			method = 0
		}
	}

	if method == 255 {
		gMetaLogger.Error("no accepted methods proposed by the client")
		return
	}

	// Send selected method
	_, err = client.Write([]byte{5, method})
	if err != nil {
		gMetaLogger.Error(err)
		return
	}
	gMetaLogger.Debugf("sending SOCKS answer, accepting method %v", method)

	// Read version, cmd, rsv and atyp
	buff = make([]byte, 4)
	_, err = io.ReadFull(reader, buff)
	if err != nil {
		gMetaLogger.Error(err)
		return
	}

	cmd := buff[1]
	atyp := buff[3]

	// Only connect command is supported
	if cmd != cmdConnect {
		gMetaLogger.Errorf("only CONNECT (0x01) SOCKS command is supported, not 0x0%v", cmd)
		client.Write([]byte{5, 7})
		return
	}

	addr, err := addrToString(reader, atyp)
	if err != nil {
		gMetaLogger.Error(err)
		client.Write([]byte{5, 1})
		return
	}

	gMetaLogger.Debugf("received SOCKS CMD packet : cmd=%v - atype=%v - addr=%s\n", cmd, atyp, addr)

	// ***** END SOCKS5 input parsing *****

	// ***** BEGIN Routing decision *****

	// Decide which chain to use based on the target address

	var chainStr string

	if gArgPACPath != "" {
		// -pac flag defined, use PAC to find the chain
		chainStr, err = getRouteWithPAC(addr)

		if err != nil {
			gMetaLogger.Errorf("error getting route PAC: %v", err)
			client.Write([]byte{5, 1})
			return
		}

	} else {
		// use JSON config to find the chain
		gRoutingConf.mu.RLock()
		chainStr, err = gRoutingConf.routing.getRoute(addr)
		gRoutingConf.mu.RUnlock()

		if err != nil {
			gMetaLogger.Errorf("error getting route with JSON conf: %v", err)
			client.Write([]byte{5, 1})
			return
		}
	}

	gMetaLogger.Debugf("chain to use for %v: %v\n", addr, chainStr)

	if chainStr == "drop" {
		gMetaLogger.Debugf("dropping connection to %v", addr)
		gMetaLogger.Auditf("| DROPPED\t| %v\t| %v\t| %v\n", &client, chainStr, addr)
		client.Write([]byte{5, 2})
		return
	}

	gChainsConf.mu.RLock()
	chain, ok := gChainsConf.proxychains[chainStr]
	gChainsConf.mu.RUnlock()

	if !ok {
		gMetaLogger.Errorf("chain '%v' is not declared in configuration", chainStr)
		client.Write([]byte{5, 1})
		return
	}

	// ***** END Routing decision *****

	// ***** BEGIN Connection to target host  *****

	//Connect to chain
	target, chainRepresentation, err := chain.connect(addr)

	if err != nil {
		gMetaLogger.Error(err)
		gMetaLogger.Auditf("| ERROR\t| %v\t| %v\t| %v\t| %v\n", &client, chainStr, addr, chainRepresentation)
		client.Write([]byte{5, 1})
		return
	}
	defer target.Close()

	gMetaLogger.Debugf("Client %v connected to host %v through chain %v", client, addr, chainStr)

	// Create auditing trace for connection opening and defering closing trace

	gMetaLogger.Auditf("| OPEN\t| %v\t| %v\t| %v\t| %v\n", &client, chainStr, addr, chainRepresentation)
	defer gMetaLogger.Auditf("| CLOSE\t| %v\t| %v\t| %v\t| %v\n", &client, chainStr, addr, chainRepresentation)

	//Terminate SOCKS5 handshake with client
	_, err = client.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	if err != nil {
		gMetaLogger.Error(err)
		return
	}
	gMetaLogger.Debugf("sent SOCKS success response")

	// ***** END Connection to target host  *****

	relay(client, target)

}

// handleHttpCONNECTConnection handles the connection of a client on the input HTTP CONNECT listener.
// It parses the CONNECT request, establishes a connection to the requested host through the right chain,
// transfers data between the established connecion socket and the clien socket, and finally closes evetything on errors or at the end.
func handleHttpCONNECTConnection(client net.Conn, ctx context.Context, cancel context.CancelFunc) {
	//debug
	gMetaLogger.Debugf("Entering handleHttpCONNNECTConnection for connection %v", &client)
	defer func() { gMetaLogger.Debugf("Leaving handleHttpCONNNECTConnection for connection %v", &client) }()

	defer client.Close()

	// ***** BEGIN HTTP CONNECT input parsing *****

	// Parse CONNECT request to retrieve target host and target port

	reader := bufio.NewReader(client)

	request, err := http.ReadRequest(reader)

	if err != nil {
		gMetaLogger.Error(err)
		return
	}

	gMetaLogger.Debug(request)
	gMetaLogger.Debugf("METHOD: %v\nURL: %v", request.Method, request.URL.Host)

	if request.Method != "CONNECT" {
		gMetaLogger.Errorf("only HTTP CONNECT method is supported")
		(&http.Response{StatusCode: 405, ProtoMajor: 1}).Write(client)
		return
	}

	if request.Host != request.URL.Host {
		gMetaLogger.Error("host and URL do not match")
		(&http.Response{StatusCode: 400, ProtoMajor: 1}).Write(client)
		return
	}

	addr := request.Host

	// ***** END HTTP CONNECT input parsing *****

	// ***** BEGIN Routing decision *****

	var chainStr string

	if gArgPACPath != "" {
		// -pac flag defined, use PAC to find the chain
		chainStr, err = getRouteWithPAC(addr)

		if err != nil {
			gMetaLogger.Errorf("error getting route PAC: %v", err)
			(&http.Response{StatusCode: 400, ProtoMajor: 1}).Write(client)
			return
		}

	} else {
		// use JSON config to find the chain
		gRoutingConf.mu.RLock()
		chainStr, err = gRoutingConf.routing.getRoute(addr)
		gRoutingConf.mu.RUnlock()

		if err != nil {
			gMetaLogger.Errorf("error getting route with JSON conf: %v", err)
			(&http.Response{StatusCode: 400, ProtoMajor: 1}).Write(client)
			return
		}
	}

	gMetaLogger.Debugf("chain to use for %v: %v\n", addr, chainStr)

	if chainStr == "drop" {
		gMetaLogger.Debugf("dropping connection to %v", addr)
		gMetaLogger.Auditf("| DROPPED\t| %v\t| %v\t| %v\n", &client, chainStr, addr)
		(&http.Response{StatusCode: 403, ProtoMajor: 1}).Write(client)
		return
	}

	gChainsConf.mu.RLock()
	chain, ok := gChainsConf.proxychains[chainStr]
	gChainsConf.mu.RUnlock()

	if !ok {
		gMetaLogger.Errorf("chain '%v' returned by PAC script is not declared in configuration", chainStr)
		(&http.Response{StatusCode: 500, ProtoMajor: 1}).Write(client)
		return
	}

	// ***** END Routing decision *****

	// ***** BEGIN Connection to target host  *****

	//Connect to chain
	target, chainRepresentation, err := chain.connect(addr)

	if err != nil {
		gMetaLogger.Error(err)
		gMetaLogger.Auditf("| ERROR\t| %v\t| %v\t| %v\t| %v\n", &client, chainStr, addr, chainRepresentation)
		(&http.Response{StatusCode: 502, ProtoMajor: 1}).Write(client)
		return
	}
	defer target.Close()

	gMetaLogger.Debugf("Client %v connected to host %v through chain %v", client, addr, chainStr)

	// Create auditing trace for connection opening and defering closing trace
	gMetaLogger.Auditf("| OPEN\t| %v\t| %v\t| %v\t| %v\n", &client, chainStr, addr, chainRepresentation)
	defer gMetaLogger.Auditf("| CLOSE\t| %v\t| %v\t| %v\t| %v\n", &client, chainStr, addr, chainRepresentation)

	// Send HTTP Success

	err = (&http.Response{StatusCode: 200, ProtoMajor: 1}).Write(client)
	if err != nil {
		gMetaLogger.Error(err)
		return
	}
	gMetaLogger.Debugf("sent HTTP success response")

	// ***** END Connection to target host  *****

	relay(client, target)

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
