package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
)

type socks5Handler struct {
	table string
}

func (h socks5Handler) String() string {
	return fmt.Sprintf("SOCKS5[%s]", h.table)
}

// connHandle handles the connection of a client on the input SOCKS5 listener.
// It parses the SOCKS command, establishes a connection to the requested host through the right chain (found in routingtable table),
// transfers data between the established connecion socket and the clien socket, and finally closes evetything on errors or at the end.
// Takes a context with its cancel function, and calls it before returning (also closes client)
func (h socks5Handler) connHandle(client net.Conn, ctx context.Context, cancel context.CancelFunc) {
	gMetaLogger.Debugf("Entering socks5Handler connHandle for connection (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	defer func() {
		gMetaLogger.Debugf("Leavings socks5Handler connHandle for connection(c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	}()

	defer client.Close()
	defer cancel()

	// ***** BEGIN SOCKS5 input parsing *****

	// Parse SOCKS5 input to retrieve command, target host and target port (see RFC 1928)

	reader := bufio.NewReader(client)

	// Read version and number of methods
	//TODO: should we also set a deadline (of tcpReadTimeout) on read operations on client side sockets ?
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
		table, ok := gRoutingConf.routing[h.table]
		if !ok {
			gMetaLogger.Errorf("table %v not defined in routing configuration", table)
			client.Write([]byte{5, 1})
			gRoutingConf.mu.RUnlock()
			return
		}
		chainStr, err = table.getRoute(addr)
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
		gMetaLogger.Auditf("| %v\t| DROPPED\t| %v\t| %v\t| %v\n", h, client, chainStr, addr)
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
	target, chainRepresentation, err := chain.connect(ctx, addr)

	if err != nil {
		gMetaLogger.Error(err)
		gMetaLogger.Auditf("| %v\t| ERROR\t| %v\t| %v\t| %v\t| %v\n", h, client, chainStr, addr, chainRepresentation)
		client.Write([]byte{5, 1})
		return
	}
	defer target.Close()

	gMetaLogger.Debugf("Client %v connected to host %v through chain %v", client, addr, chainStr)

	// Create auditing trace for connection opening and defering closing trace

	gMetaLogger.Auditf("| %v\t| OPEN\t| %v\t| %v\t| %v\t| %v\n", h, client, chainStr, addr, chainRepresentation)
	defer gMetaLogger.Auditf("| %v\t| CLOSE\t| %v\t| %v\t| %v\t| %v\n", h, client, chainStr, addr, chainRepresentation)

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
