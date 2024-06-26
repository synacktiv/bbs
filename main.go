package main

import (
	"github.com/synacktiv/bbs/logger"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var gChainsConf chainsConf
var gRoutingConf routingConf
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

	// Wait for data on the previously created channel to reload configuration files
	go func() {
		for {
			sig := <-signalCh
			gMetaLogger.Infof("Signal %v received, reloading configurations", sig)

			confProxyChains, err := parseConfig(gArgConfigPath)
			if err != nil {
				gMetaLogger.Errorf("error parsing JSON configuration: %v", err)
				continue
			}

			gChainsConf.mu.Lock()
			gChainsConf.proxychains = confProxyChains
			gChainsConf.valid = true
			gChainsConf.mu.Unlock()
			gMetaLogger.Info("Global JSON configuration updated")

			// If -pac is not defined, use JSON routing configuration file
			if gArgPACPath == "" {
				routingConfig, err := parseRoutingConfig(gArgRoutingConfigPath)
				if err != nil {
					gMetaLogger.Errorf("error parsing JSON routing configuration: %v", err)
					continue
				}
				gRoutingConf.mu.Lock()
				gRoutingConf.routing = routingConfig
				gRoutingConf.valid = true
				gRoutingConf.mu.Unlock()
				gMetaLogger.Info("Global JSON routing configuration updated")
				gMetaLogger.Debugf("routing config: %v", routingConfig)
			} else { // Otherwise, use PAC file
				err := reloadPACConf(gArgPACPath)
				if err != nil {
					gMetaLogger.Errorf("error reloading pac file: %v", err)
					continue
				}
				gMetaLogger.Info("Global PAC configuration updated")
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

		}
	}()

	// Send a SIGHUP to trigger initial configuration loading
	signalCh <- syscall.SIGHUP

	// Wait for a first valid configuration to be loaded
	gMetaLogger.Info("Waiting for valid configuration file")
	valid := false
	for !valid {
		gChainsConf.mu.RLock()
		valid = gChainsConf.valid
		gChainsConf.mu.RUnlock()
		time.Sleep(1 * time.Second)
	}
	gMetaLogger.Info("Valid configuration loaded, running server(s)")

	// ***** END Configuration files loading *****

	// ***** BEGIN Run server *****

	// Start a HTTP CONNECT proxy listenner
	if gArgHttpListen != "" {
		go run(gArgHttpListen, HTTPCONNECT)
	}
	// Start a SOCKS5 proxy listenner
	run(gArgListen, SOCKS5)

	// ***** END Run server *****

}
