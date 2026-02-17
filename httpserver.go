package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
)

type httpHandler struct {
	table string
}

func (h httpHandler) String() string {
	return fmt.Sprintf("HTTP[%s]", h.table)
}

// connHandle handles the connection of a client on the input HTTP CONNECT listener.
// It parses the CONNECT request, establishes a connection to the requested host through the right chain (found in routingtable table),
// transfers data between the established connecion socket and the clien socket, and finally closes evetything on errors or at the end.
// Takes a context with its cancel function, and calls it before returning (also closes client)
func (h httpHandler) connHandle(client net.Conn, ctx context.Context, cancel context.CancelFunc) {
	gMetaLogger.Debugf("Entering httpHandler connHandle for connection (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	defer func() {
		gMetaLogger.Debugf("Leaving httpHandler connHandle for connection (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	}()

	defer client.Close()
	defer cancel()

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
		resp := &http.Response{StatusCode: 405, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
		resp.Write(client)
		return
	}

	if request.Host != request.URL.Host {
		gMetaLogger.Error("host and URL do not match")
		resp := &http.Response{StatusCode: 400, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
		resp.Write(client)
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
			resp := &http.Response{StatusCode: 400, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
			resp.Write(client)
			return
		}

	} else {
		// use JSON config to find the chain
		gRoutingConf.mu.RLock()
		table, ok := gRoutingConf.routing[h.table]
		if !ok {
			gMetaLogger.Errorf("table %v not defined in routing configuration", table)
			resp := &http.Response{StatusCode: 400, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
			resp.Write(client)
			gRoutingConf.mu.RUnlock()
			return
		}
		chainStr, err = table.getRoute(addr)
		gRoutingConf.mu.RUnlock()

		if err != nil {
			gMetaLogger.Errorf("error getting route with JSON conf: %v", err)
			resp := &http.Response{StatusCode: 400, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
			resp.Write(client)
			return
		}
	}

	gMetaLogger.Debugf("chain to use for %v: %v\n", addr, chainStr)

	if chainStr == "drop" {
		gMetaLogger.Debugf("dropping connection to %v", addr)
		gMetaLogger.Auditf("| %v\t| DROPPED\t| %v\t| %v\t| %v\n", h, client, chainStr, addr)
		resp := &http.Response{StatusCode: 403, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
		resp.Write(client)
		return
	}

	gChainsConf.mu.RLock()
	chain, ok := gChainsConf.proxychains[chainStr]
	gChainsConf.mu.RUnlock()

	if !ok {
		gMetaLogger.Errorf("chain '%v' returned by PAC script is not declared in configuration", chainStr)
		resp := &http.Response{StatusCode: 500, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
		resp.Write(client)
		return
	}

	// ***** END Routing decision *****

	// ***** BEGIN Connection to target host  *****

	//Connect to chain
	target, chainRepresentation, err := chain.connect(ctx, addr)

	if err != nil {
		gMetaLogger.Error(err)
		gMetaLogger.Auditf("| %v\t| ERROR\t| %v\t| %v\t| %v\t| %v\n", h, client, chainStr, addr, chainRepresentation)
		resp := &http.Response{StatusCode: 502, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
		resp.Write(client)
		return
	}
	defer target.Close()

	gMetaLogger.Debugf("Client %v connected to host %v through chain %v", client, addr, chainStr)

	// Create auditing trace for connection opening and defering closing trace
	gMetaLogger.Auditf("| %v\t| OPEN\t| %v\t| %v\t| %v\t| %v\n", h, client, chainStr, addr, chainRepresentation)
	defer gMetaLogger.Auditf("| %v\t| CLOSE\t| %v\t| %v\t| %v\t| %v\n", h, client, chainStr, addr, chainRepresentation)

	// Send HTTP Success with matching protocol version
	resp := &http.Response{StatusCode: 200, ProtoMajor: request.ProtoMajor, ProtoMinor: request.ProtoMinor}
	err = resp.Write(client)
	if err != nil {
		gMetaLogger.Error(err)
		return
	}
	gMetaLogger.Debugf("sent HTTP success response")

	// ***** END Connection to target host  *****

	relay(client, target)

}
