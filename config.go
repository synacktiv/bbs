package main

// Defines a function to parse the JSON proxies and chains configuration file and a structure to store the parsed configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type chainsConf struct {
	proxychains map[string]proxyChain
	valid       bool // whether the current configuration is valid
	mu          sync.RWMutex
}

// parseConfig parses the JSON proxies and chains configuration file at configPath and returns a map of proxyChain variables representing this configuration
func parseConfig(configPath string) (confProxyChains map[string]proxyChain, err error) {

	confProxies := make(map[string]proxy)
	confProxyChains = make(map[string]proxyChain)

	var confMap map[string]interface{}
	fileBytes, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	err = json.Unmarshal(fileBytes, &confMap)
	if err != nil {
		return
	}

	gMetaLogger.Debugf("Loaded configuration map : \n%v", confMap)

	proxies, ok := confMap["proxies"]
	if ok {
		proxies, ok := proxies.(map[string]interface{})
		if ok {
			gMetaLogger.Debugf("ok, proxies : %v", proxies)
			for proxyName, proxyConf := range proxies {
				gMetaLogger.Debugf("proxyName: %v, proxyConf: %v", proxyName, proxyConf)
				proxyConf, ok := proxyConf.(map[string]interface{})
				if ok {
					gMetaLogger.Debug(proxyConf)

					prot, ok := proxyConf["prot"].(string)
					if !ok {
						err = fmt.Errorf("'prot' missing in %v conf", proxyName)
					}

					host, ok := proxyConf["host"].(string)
					if !ok {
						err = fmt.Errorf("'host' missing in %v conf", proxyName)
					}

					port, ok := proxyConf["port"].(string)
					if !ok {
						err = fmt.Errorf("'port' missing in %v conf", proxyName)
					}

					user, _ := proxyConf["user"].(string)
					pass, _ := proxyConf["pass"].(string)

					// switch prot {
					// case "socks5":
					// 	confProxies[proxyName] = &socks5{baseProxy{prot: "socks5", host: host, port: port, user: user, pass: pass}}

					// case "httpconnect":
					// 	confProxies[proxyName] = &httpConnect{baseProxy{prot: "http", host: host, port: port, user: user, pass: pass}}
					// default:
					// 	err = fmt.Errorf("unknown proxy protocol %v in proxy %v", prot, proxyName)
					// 	return
					// }

					var err2 error
					confProxies[proxyName], err2 = newProxy(prot, host, port, user, pass)
					if err2 != nil {
						err = fmt.Errorf("error get new proxy %v: %v", proxyName, err2)
						return
					}

					gMetaLogger.Debugf("%v", confProxies[proxyName])
				} else {
					err = fmt.Errorf("%v is not a valid proxy declaration, it should be [proxies.%v]", proxyName, proxyName)
					return
				}
			}
		} else {
			err = fmt.Errorf("[proxies] section's structure is broken")
			return
		}
	} else {
		gMetaLogger.Debugf("here")
		err = fmt.Errorf("[proxies] section missing")
		return
	}

	chains, ok := confMap["chains"]
	if ok {
		chains, ok := chains.(map[string]interface{})
		if ok {
			gMetaLogger.Debugf("ok, chains : %v", chains)
			for chainName, chainConf := range chains {
				gMetaLogger.Debugf("chainName: %v, chainConf: %v", chainName, chainConf)
				chainConf, ok := chainConf.(map[string]interface{})
				if ok {
					gMetaLogger.Debug(chainConf)

					proxyDns, ok := chainConf["proxyDns"].(bool)
					if !ok {
						gMetaLogger.Debugf("Cannot parse chain %v's 'proxyDns' param. Using false as default.", chainName)
						proxyDns = false
					}

					tcpConnectTimeout := int64(15000)
					tcpConnectTimeoutF, ok := chainConf["tcpConnectTimeout"].(float64)
					if !ok {
						gMetaLogger.Debugf("Cannot parse chain %v's 'tcpConnectTimeout' param. Using 15000 as default.", chainName)
					} else {
						tcpConnectTimeout = int64(tcpConnectTimeoutF)
					}

					tcpReadTimeout := int64(8000)
					tcpReadTimeoutF, ok := chainConf["tcpReadTimeout"].(float64)
					if !ok {
						gMetaLogger.Debugf("Cannot parse chain %v's 'tcpReadTimeout' param. Using 8000 as default.", chainName)
					} else {
						tcpReadTimeout = int64(tcpReadTimeoutF)
					}

					proxychain := proxyChain{proxyDns: proxyDns, tcpConnectTimeout: tcpConnectTimeout, tcpReadTimeout: tcpReadTimeout}

					_, ok = chainConf["proxies"]
					if !ok {
						err = fmt.Errorf("'proxies' key missing in chain %v", chainName)
					}

					proxyNames, ok := chainConf["proxies"].([]interface{})
					if ok {
						gMetaLogger.Debug(proxies)
						for _, proxyName := range proxyNames {
							gMetaLogger.Debug(proxyName)
							proxyName, ok := proxyName.(string)
							if !ok {
								err = fmt.Errorf("elements of list 'proxies' should be proxy names, in chain %v", chainName)
								return
							}
							proxy, ok := confProxies[proxyName]
							if !ok {
								err = fmt.Errorf("chain '%v' uses proxy '%v' which is not declared in [proxies] section", chainName, proxyName)
							}

							proxychain.proxies = append(proxychain.proxies, proxy)
						}
					} else {
						err = fmt.Errorf("'proxies' key of chain '%v' should be a list of proxies like [\"proxy1\", \"proxy2\"]", chainName)
						return
					}

					confProxyChains[chainName] = proxychain

				} else {
					err = fmt.Errorf("%v is not a valid chain declaration, it should be [chains.%v]", chainName, chainName)
				}
			}

		} else {
			err = fmt.Errorf("[chains] section's structure is broken")
			return
		}
	} else {
		err = fmt.Errorf("[chains] section is missing")
		return
	}

	return
}
