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
var gHosts map[string]string
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

		var servers []server
		var routing routing

		gMetaLogger.Debug("Describing gServerConf.servers : ")
		describeServers(gServerConf.servers)

		// Load proxies and chains configuration
		confProxyChains, err := parseConfig(gArgConfigPath)
		if err != nil {
			gMetaLogger.Errorf("error parsing JSON configuration: %v", err)
			continue
		}

		// If -pac is not defined, load JSON routing configuration file, load servers and perform consistency checks
		if gArgPACPath == "" {
			routing, err = parseRoutingConfig(gArgRoutingConfigPath)
			if err != nil {
				gMetaLogger.Errorf("error parsing JSON routing configuration: %v", err)
				continue
			}

			// Check that all routes defined in routing correspond to an existing chain in confProxyChains
			allExist := true
			definedRoutes := slices.Collect(maps.Keys(confProxyChains))
			for _, routingTable := range routing {
				for _, ruleBlock := range routingTable {

					if !slices.Contains(definedRoutes, ruleBlock.Route) {
						gMetaLogger.Errorf("route %v defined in routing configuration is not defined in the chain configuration (%v)", ruleBlock.Route, definedRoutes)
						allExist = false
					}
				}
			}

			if !allExist {
				continue
			}

			// Load server configuration
			servers, err = parseServerConfig("server.json")
			if err != nil {
				gMetaLogger.Errorf("error parsing JSON server configuration: %v", err)
				continue
			}

			// Check that all servers defined use an existing routing table
			allExist = true
			definedTables := slices.Collect(maps.Keys(routing))
			for _, server := range servers {
				if !slices.Contains(definedTables, server.table) {
					gMetaLogger.Errorf("table %v defined in server configuration is not defined in the routing configuration (%v)", server.table, definedTables)
					allExist = false
				}
			}

			if !allExist {
				continue
			}

		} else { // Otherwise, load PAC file, load servers,  and do not perform consistency checks
			err := reloadPACConf(gArgPACPath)
			if err != nil {
				gMetaLogger.Errorf("error reloading pac file: %v", err)
				continue
			}
			gMetaLogger.Info("Global PAC configuration updated")

			// Load server configuration
			servers, err = parseServerConfig("server.json")
			if err != nil {
				gMetaLogger.Errorf("error parsing JSON server configuration: %v", err)
				continue
			}
		}

		// Load custom host resolution config file
		if gArgCustomHosts != "" {
			var tmpHosts map[string]string
			tmpHosts, err = loadHosts(gArgCustomHosts)
			if err != nil {
				gMetaLogger.Errorf("error parsing custom hosts file: %v", err)
				continue
			}
			gHosts = tmpHosts
			gMetaLogger.Info("Global host resolution configuration updated")
		}

		// At this point, the defined configuration should be consistent, so we can update the globals

		// Update global variables if all the configuration files are consistent
		gChainsConf.mu.Lock()
		gChainsConf.proxychains = confProxyChains
		gChainsConf.valid = true
		gChainsConf.mu.Unlock()
		gMetaLogger.Info("Global JSON configuration updated")

		if gArgPACPath == "" {
			gRoutingConf.mu.Lock()
			gRoutingConf.routing = routing
			gRoutingConf.valid = true
			gRoutingConf.mu.Unlock()
			gMetaLogger.Info("Global JSON routing configuration updated")
			gMetaLogger.Debugf("routing config: %v", routing)
		}

		// Update global servers variable, stop old ones and start new ones

		// Stoping running servers that are not defined in the new configuration
		gMetaLogger.Debug("Describing servers : ")
		describeServers(servers)
		gServerConf.mu.Lock()
		for i := range gServerConf.servers {
			stillExists := slices.ContainsFunc(servers, func(s server) bool { return compare(s, gServerConf.servers[i]) })
			if stillExists {
				gMetaLogger.Debugf("Server %v still exists in new loaded servers, keeping it", gServerConf.servers[i])
			} else {
				gMetaLogger.Debugf("Server %v does not exists anymore, stopping it", gServerConf.servers[i])
				gServerConf.servers[i].stop()
				gServerConf.servers = slices.Delete(gServerConf.servers, i, i+1)

			}
		}

		for i := range servers {
			alreadyExists := slices.ContainsFunc(gServerConf.servers, func(s server) bool { return compare(s, servers[i]) })
			if !alreadyExists {
				gServerConf.servers = append(gServerConf.servers, servers[i])
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
