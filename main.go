package main

import (
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/synacktiv/bbs/logger"
)

var gChainsConf chainsConf
var gRoutingConf routingConf
var gServerConf serverConf
var gHosts hostMap
var gMetaLogger *logger.MetaLogger

func main() {

	// Parse the command line arguments
	parseArgs()

	// ***** BEGIN Logs setup *****

	var auditFile *os.File = nil
	var logFile *os.File = nil

	if gArgAuditPath != "" {
		var err error
		auditFile, err = os.OpenFile(gArgAuditPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
		if err != nil {
			panic(err)
		}
		defer auditFile.Close()
	}

	if gArgLogPath != "" {
		var err error
		logFile, err = os.OpenFile(gArgLogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
		if err != nil {
			panic(err)
		}
		defer logFile.Close()
	}

	var logWriter io.Writer = os.Stdout
	var auditWriter io.Writer = os.Stdout

	if auditFile != nil {
		if gArgAuditBoth {
			auditWriter = io.MultiWriter(os.Stdout, auditFile)
		} else {
			auditWriter = auditFile
		}
	}

	if logFile != nil {
		if gArgLogBoth {
			logWriter = io.MultiWriter(os.Stdout, logFile)
		} else {
			logWriter = logFile
		}
	}

	gMetaLogger = logger.NewMetaLogger(logWriter, auditWriter)

	if gArgQuietBool {
		gMetaLogger.SetLogLevel(logger.LogLevelQuiet)
	} else if gArgVerboseBool {
		gMetaLogger.SetLogLevel(logger.LogLevelVerbose)
	} else {
		gMetaLogger.SetLogLevel(logger.LogLevelNormal)
	}

	if gArgNoAuditBool {
		gMetaLogger.SetAuditLevel(logger.AuditLevelNo)
	} else {
		gMetaLogger.SetAuditLevel(logger.AuditLevelYes)
	}

	// ***** END Logs setup *****

	// ***** BEGIN Configuration files loading *****

	// Output PID needed to hot reload configuration files
	gMetaLogger.Infof("bbs PID: %v. Use the following to reload configuration:", os.Getpid())
	gMetaLogger.Infof("kill -HUP %v", os.Getpid())

	// Setup a notification channel listening on SIGHUP, used to hot reload configuration files
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGHUP)

	// Send a SIGHUP to trigger initial configuration loading
	signalCh <- syscall.SIGHUP

	// Wait for data on the previously created channel to reload configuration files
	for {
		sig := <-signalCh
		gMetaLogger.Infof("Signal %v received, reloading configurations", sig)

		gMetaLogger.Debug("Describing gServerConf.servers : ")
		describeServers(gServerConf.servers)

		// Load main config from the unified config file (proxies, chains, routes, servers and hosts)
		config, err := parseMainConfig(gArgConfigPath)
		if err != nil {
			gMetaLogger.Errorf("error parsing main config : %v", err)
			continue
		}
		gMetaLogger.Debugf("Successfully parsed main config : %v", config)

		// Check that all proxies used in all chains fo chains section correspond to an existing proxy in the proxies section
		allExist := true
		definedProxies := slices.Collect(maps.Keys(config.Proxies))
		for chainName, chainDesc := range config.Chains {
			for index, proxyName := range chainDesc.Proxies {
				if !slices.Contains(definedProxies, proxyName) {
					gMetaLogger.Errorf("proxy %v used at index %v of chain %v is not part of the defined proxies in proxies section (%v)", proxyName, index, chainName, definedProxies)
					allExist = false
				}
			}
		}
		if !allExist {
			continue
		}

		// If -pac is not defined, perform consistency checks on routing configuration
		if gArgPACPath == "" {

			// Check that all routes defined in routes section correspond to an existing chain in the chains section
			allExist = true
			definedChains := slices.Collect(maps.Keys(config.Chains))
			for routingTableName, routingTable := range config.Routes {
				for index, ruleBlock := range routingTable {

					if ruleBlock.Route != "drop" && !slices.Contains(definedChains, ruleBlock.Route) {
						gMetaLogger.Errorf("route %v defined in ruleBlock number %v of routingTable %v is not part of the defined chains in the chains section (%v)", ruleBlock.Route, index, routingTableName, definedChains)
						allExist = false
					}
				}
			}
			if !allExist {
				continue
			}

			// Check that all routing tables used in all servers of the servers sections correspond to an existing routing table in the routes section
			allExist = true
			definedRoutingTables := slices.Collect(maps.Keys(config.Routes))
			for index, server := range config.Servers {
				if !slices.Contains(definedRoutingTables, server.table) {
					gMetaLogger.Errorf("table %v used by server number %v is not part of the defined routing tables in section routes (%v)", server.table, index, definedRoutingTables)
					allExist = false
				}
			}
			if !allExist {
				continue
			}

		} else { // Otherwise, load PAC file and do not perform consistency checks
			err := reloadPACConf(gArgPACPath)
			if err != nil {
				gMetaLogger.Errorf("error reloading pac file: %v", err)
				continue
			}
			gMetaLogger.Info("Global PAC configuration updated")
		}

		// At this point, the defined configuration should be consistent, so we can update the globals

		// Build a proxyChain object from the proxyChainDesc parsed in JSON file

		proxychains := make(map[string]proxyChain)

		for chainName, chainDesc := range config.Chains {
			var proxychain proxyChain
			proxychain.proxyDns = chainDesc.ProxyDns
			proxychain.tcpConnectTimeout = chainDesc.TcpConnectTimeout
			proxychain.tcpReadTimeout = chainDesc.TcpReadTimeout

			for _, proxyName := range chainDesc.Proxies {
				proxychain.proxies = append(proxychain.proxies, config.Proxies[proxyName])
			}

			proxychains[chainName] = proxychain

		}
		gChainsConf.mu.Lock()
		gChainsConf.proxychains = proxychains
		gChainsConf.valid = true
		gChainsConf.mu.Unlock()
		gMetaLogger.Info("Global chains configuration updated")

		gHosts = config.Hosts
		gMetaLogger.Info("Global hosts configuration updated")

		if gArgPACPath == "" {
			gRoutingConf.mu.Lock()
			gRoutingConf.routing = config.Routes
			gRoutingConf.valid = true
			gRoutingConf.mu.Unlock()
			gMetaLogger.Info("Global routing configuration updated")
			gMetaLogger.Debugf("routing config: %v", config.Routes)
		}

		// Update global servers variable, stop old ones and start new ones

		// Stoping running servers that are not defined in the new configuration
		gMetaLogger.Debug("Describing servers : ")
		describeServers(config.Servers)
		gServerConf.mu.Lock()
		for i := range gServerConf.servers {
			stillExists := slices.ContainsFunc(config.Servers, func(s server) bool { return compare(s, gServerConf.servers[i]) })
			if stillExists {
				gMetaLogger.Debugf("Server %v still exists in new loaded servers, keeping it", gServerConf.servers[i])
			} else {
				gMetaLogger.Debugf("Server %v does not exists anymore, stopping it", gServerConf.servers[i])
				gServerConf.servers[i].stop()
				gServerConf.servers = slices.Delete(gServerConf.servers, i, i+1)

			}
		}

		for i := range config.Servers {
			alreadyExists := slices.ContainsFunc(gServerConf.servers, func(s server) bool { return compare(s, config.Servers[i]) })
			if !alreadyExists {
				gServerConf.servers = append(gServerConf.servers, config.Servers[i])
			}
		}

		gServerConf.mu.Unlock()

		gMetaLogger.Debugf("gServerConf.servers : %v", gServerConf.servers)
		gMetaLogger.Debug("Describing gServerConf.servers : ")
		describeServers(gServerConf.servers)

		// Start all servers that are not running
		for i := 0; i < len(gServerConf.servers); i++ {
			if !gServerConf.servers[i].running {
				gMetaLogger.Debugf("myServer %v(%p) is not running, running it", gServerConf.servers[i], &gServerConf.servers[i])
				go (gServerConf.servers[i]).run()
				time.Sleep(1 * time.Second)
				gMetaLogger.Debugf("myServer %v(%p) is running", gServerConf.servers[i], &gServerConf.servers[i])
			}
		}

		gMetaLogger.Debug("Describing gServerConf.servers : ")
		describeServers(gServerConf.servers)

	}
}
