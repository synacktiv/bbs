package main

import (
	"context"
	"fmt"
	"net"
)

type forwardHandler struct {
	dest  string
	chain string
}

func (h forwardHandler) String() string {
	return fmt.Sprintf("Fwd[%s:%s]", h.chain, h.dest)
}

func (h forwardHandler) connHandle(client net.Conn, ctx context.Context, cancel context.CancelFunc) {
	gMetaLogger.Debugf("Entering forwardHandler connHandle for connection (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	defer func() {
		gMetaLogger.Debugf("Leaving forwardHandler connHandle for connection (c[%%+v]: %+v, c(%%p): %p &c(%%v): %v) accepted", client, client, &client)
	}()

	defer client.Close()

	// ***** BEGIN Connection to target host  *****

	gChainsConf.mu.RLock()
	chain, ok := gChainsConf.proxychains[h.chain]
	gChainsConf.mu.RUnlock()

	if !ok {
		gMetaLogger.Errorf("chain '%v' used by forwarder does not exists in config", h.chain)
		return
	}

	//Connect to chain

	target, chainRepresentation, err := chain.connect(ctx, h.dest)

	if err != nil {
		gMetaLogger.Error(err)
		gMetaLogger.Auditf("| %v\t| ERROR\t| %v\t| %v\t| %v\t| %v\n", h, client, h.chain, h.dest, chainRepresentation)
		return
	}
	defer target.Close()

	gMetaLogger.Debugf("Client %v connected to host %v through chain %v", client, h.dest, h.chain)

	// Create auditing trace for connection opening and defering closing trace
	gMetaLogger.Auditf("| %v\t| OPEN\t| %v\t| %v\t| %v\t| %v\n", h, client, h.chain, h.dest, chainRepresentation)
	defer gMetaLogger.Auditf("| %v\t| CLOSE\t| %v\t| %v\t| %v\t| %v\n", h, client, h.chain, h.dest, chainRepresentation)

	// ***** END Connection to target host  *****

	relay(client, target)
}
