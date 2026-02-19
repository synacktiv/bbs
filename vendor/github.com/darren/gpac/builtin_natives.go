package gpac

import (
	"net"

	"github.com/dop251/goja"
)

var builtinNatives = map[string]func(*goja.Runtime) func(call goja.FunctionCall) goja.Value{
	"dnsResolve":  dnsResolve,
	"myIpAddress": myIPAddress,
}

func dnsResolve(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		arg := call.Argument(0)
		if arg == nil || arg.Equals(goja.Undefined()) {
			return goja.Null()
		}

		host := arg.String()
		ips, err := net.LookupIP(host)
		if err != nil {
			return goja.Null()
		}

		return vm.ToValue(ips[0].String())
	}
}

func myIPAddress(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		ifs, err := net.Interfaces()
		if err != nil {
			return goja.Null()
		}

		for _, ifn := range ifs {
			if ifn.Flags&net.FlagUp != net.FlagUp {
				continue
			}

			addrs, err := ifn.Addrs()
			if err != nil {
				continue
			}

			for _, addr := range addrs {
				ip, ok := addr.(*net.IPNet)
				if ok && ip.IP.IsGlobalUnicast() {
					ipstr := ip.IP.String()
					return vm.ToValue(ipstr)
				}
			}
		}
		return goja.Null()
	}
}
