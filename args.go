package main

// Defines the command line parsing function and global variables

import (
	"flag"
	"fmt"
	"os"
)

type servers []string

var gArgLogPath string
var gArgAuditPath string
var gArgAuditBoth bool
var gArgLogBoth bool
var gArgNoAuditBool bool

var gArgConfigPath string
var gArgPACPath string
var gArgRoutingConfigPath string

var gArgQuietBool bool
var gArgVerboseBool bool

var gArgCustomHosts string

func cmdlineError(a ...interface{}) {
	fmt.Fprintln(os.Stderr, a...)
	os.Exit(1)
}

func (ss *servers) String() string {
	if ss != nil {
		str := ""
		for _, s := range *ss {
			str += s + " "
		}
	}
	return ""
}

func (ss *servers) Set(str string) error {

	*ss = append(*ss, str)
	fmt.Fprintln(os.Stdout, "added new server")

	return nil
}

// parseArgs parses the command line arguments, performs some checks, and store them in the associated global variables
func parseArgs() {
	flag.BoolVar(&gArgQuietBool, "q", false, "Quiet mode")
	flag.BoolVar(&gArgVerboseBool, "v", false, "Verbose mode")
	//flag.StringVar(&gArgListen, "socks", "127.0.0.1:8851", "Address and port to listen on (format: local_addr:local_port)")
	//flag.StringVar(&gArgHttpListen, "http", "", "Address and port to listen on for HTTP CONNECT input (format: local_addr:local_port) (no http listener by default)")
	flag.StringVar(&gArgAuditPath, "audit-file", "", "File to output audit traces. Output to STDOUT if empty")
	flag.BoolVar(&gArgAuditBoth, "audit-both", false, "Output audit traces to both -audit-file and STDOUT.")
	flag.StringVar(&gArgLogPath, "log-file", "", "File to output logs. Output to STDOUT if empty")
	flag.BoolVar(&gArgLogBoth, "log-both", false, "Output logs to both -log-file and STDOUT.")
	flag.StringVar(&gArgConfigPath, "c", "./bbs.json", "JSON configuration file path")
	flag.BoolVar(&gArgNoAuditBool, "no-audit", false, "No audit traces mode")
	flag.StringVar(&gArgCustomHosts, "custom-hosts", "", "File to define custom hosts IP (like /etc/hosts)")
	if gPACcompiled {
		flag.StringVar(&gArgRoutingConfigPath, "routes", "", "JSON routing configuration file path")
		flag.StringVar(&gArgPACPath, "pac", "", "PAC script file path")
	} else {
		flag.StringVar(&gArgRoutingConfigPath, "routes", "./routes.json", "JSON routing configuration file path")
	}

	flag.Parse()

	if gArgQuietBool && gArgVerboseBool {
		cmdlineError("Arguments -q and -v cannot be used together")
	}

	if gArgAuditBoth && gArgAuditPath == "" {
		cmdlineError("-audit-file must be defined if -audit-both is set")
	}

	if gArgLogBoth && gArgLogPath == "" {
		cmdlineError("-log-file must be defined if -log-both is set")
	}

	if (gArgNoAuditBool && gArgAuditBoth) || (gArgNoAuditBool && gArgAuditPath != "") {
		cmdlineError("Arguments -no-audit and -audit-file/-audit-both cannot be used together")
	}

	if gPACcompiled {
		if (gArgPACPath != "") && (gArgRoutingConfigPath != "") {
			cmdlineError("Arguments -pac and -routes cannot be used together")
		}
		if (gArgPACPath == "") && (gArgRoutingConfigPath == "") {
			cmdlineError("At least one of -pac and -routes must be defined")
		}
	}

}
