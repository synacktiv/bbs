package main

import (
	"bufio"
	"context"
	"net"
	"net/http"
)

type httpHandler struct{}

// connHandle handles the connection of a client on the input HTTP CONNECT listener.
// It parses the CONNECT request, establishes a connection to the requested host through the right chain (found in routingtable table),
// transfers data between the established connecion socket and the clien socket, and finally closes evetything on errors or at the end.
func (h httpHandler) connHandle(client net.Conn, table string, ctx context.Context, cancel context.CancelFunc) {
	gMetaLogger.Debugf("Entering httpHandler connHandle for connection (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	defer func() {
		gMetaLogger.Debugf("Leaving httpHandler connHandle for connection (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	}()

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
		table, ok := gRoutingConf.routing[table]
		if !ok {
			gMetaLogger.Errorf("table %v not defined in routing configuration", table)
			(&http.Response{StatusCode: 400, ProtoMajor: 1}).Write(client)
			gRoutingConf.mu.RUnlock()
			return
		}
		chainStr, err = table.getRoute(addr)
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
		gMetaLogger.Auditf("| DROPPED\t| %v\t| %v\t| %v\n", client, chainStr, addr)
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
	target, chainRepresentation, err := chain.connect(ctx, addr)

	if err != nil {
		gMetaLogger.Error(err)
		gMetaLogger.Auditf("| ERROR\t| %v\t| %v\t| %v\t| %v\n", client, chainStr, addr, chainRepresentation)
		(&http.Response{StatusCode: 502, ProtoMajor: 1}).Write(client)
		return
	}
	defer target.Close()

	gMetaLogger.Debugf("Client %v connected to host %v through chain %v", client, addr, chainStr)

	// Create auditing trace for connection opening and defering closing trace
	gMetaLogger.Auditf("| OPEN\t| %v\t| %v\t| %v\t| %v\n", client, chainStr, addr, chainRepresentation)
	defer gMetaLogger.Auditf("| CLOSE\t| %v\t| %v\t| %v\t| %v\n", client, chainStr, addr, chainRepresentation)

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
