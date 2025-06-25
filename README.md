# BBS

The old `bbs` can be found [here](https://github.com/synacktiv/bbs/tree/v1.0)

## Description

`bbs` is a router for SOCKS and HTTP proxies. It exposes SOCKS5, HTTP
CONNECT or port forwarding services and forwards incoming requests to proxies or chains of proxies
based on the request's target. Routing can be configured with a PAC script (if
built with PAC support), or through a JSON file.

## Install 

```bash
go install github.com/synacktiv/bbs@master
```

To install bbs with PAC script support: 
```bash
go install -tags pac github.com/synacktiv/bbs@master
```

Note: PAC relies on unaudited third-party libraries.


## Configuration

Configuration is performed in one JSON file composed of multiple sections:

- Proxies: defines all the upstream proxies used by bbs
- Chains: defines the differents chains of previously defined proxies, and their settings
- Routes: defines the different routing tables 
- Servers: defines the listeners (SOCKS5, HTTP or port forwarding) opened by bbs
- Hosts: defines custom hosts resolution (in a /etc/hosts way)


The configuration file path is provided through argument `-c <path>` (default to `./bbs.json`).
`bbs` reloads configuration files on SIGHUP, use `kill -HUP <pid>` to reload.

Here is an example of such configuration:

```json
{
  "proxies": {
    "proxy1": {
      "connstring": "socks5://127.0.0.1:1337",
      "user": "user",
      "pass": "s3cr3t"
    },
    "proxy2": {
      "connstring": "http://127.0.0.1:1338"
    }
  },
  "chains": {
    "chain1": {
      "proxyDns": true,
      "tcpConnectTimeout": 1000,
      "tcpReadTimeout": 2000,
      "proxies": [
        "proxy1",
        "proxy2"
      ]
    },
    "direct": {
      "proxies": []
    }
  },
  "routes": {
    "table1": {
      "default": "direct",
      "blocks": [
        {
          "comment": "Block1 comment",
          "rules": {
            "rule": "regexp",
            "variable": "host",
            "content": "me\\.gandi\\.net"
          },
          "route": "chain1"
        },
        {
          "comment": "Route non web traffic towards 10.35.0.0/16 through proxy2",
          "rules": {
            "rule1": {
              "rule": "subnet",
              "content": "10.35.0.0/16"
            },
            "op": "AND",
            "rule2": {
              "rule": "regexp",
              "variable": "port",
              "content": "^(80|443)$",
              "negate": "true"
            }
          },
          "route": "proxy2"
        },
        {
          "comment": "Drop traffic to 445",
          "rules": {
            "rule": "regexp",
            "variable": "port",
            "content": "^445$"
          },
          "route": "drop"
        },
        {
          "comment": "Route *.corp.local through chain1",
          "rules": {
            "rule": "regexp",
            "variable": "host",
            "content": "(?i)^(.*\\.)?corp\\.local$"
          },
          "route": "chain1",
          "disable": "true"
        }
      ]
    },
    "table2": {
      "default": "drop",
      "blocks": [
        {
          "comment": "Route *.corp.local through chain2",
          "rules": {
            "rule": "regexp",
            "variable": "host",
            "content": "(?i)^(.*\\.)?corp\\.local$"
          },
          "route": "chain2"
        }     
      ]
    }
  },
  "servers": [
    "socks5://127.0.0.1:1081:table1",
    "http://127.0.0.1:1080:table2",
    "fwd://127.0.0.1:4445:chain1:10.0.0.1:445"
  ],
  "hosts": {
    "host1": "1.1.1.1",
    "host2": "10.0.0.1",
    "host3": "modified.host3",
    "10.1.1.4": "10.1.1.5"
  }
}
```

### Proxies 

Upstream proxies must be declared in the `proxies` section as a map of proxy
structures. Map keys are chosen freely but must match the ones used in chains 
definition. Proxy structures are like this:

- `connstring` is required with format `protocol://host:port` (`protocol` can be `socks5` or `httpconnect`/`http`)
- `user` and `pass` are optional

For each proxy declared, an implicit chain (see next paragraph) is created with 
the same name. It has default parameters and is composed of the single associated
proxy. If you want to use non-default parameters, you must explicitely create a chain.

### Chains

Chains must be declared in the `chains` section as a map of chain structures.
Map keys are chosen freely by must match with the ones used in routes definition, and 
must be different than the `proxies` section map keys.
Chain structures have proxychains-like parameters (cf. https://github.com/rofl0r/proxychains-ng):

- `proxyDns`: boolean, optional, defaults to `true`
- `tcpConnectTimeout`: integer, optional, defaults to 1000 (used when connecting sockets, either to the first proxy
  of the chain or directly to the target)
- `tcpReadTimeout`: integer, optional, defaults to 2000 (used when reading proxy handshakes responses on connected sockets)
- `proxies`: string list, optional, defaults to empty list

The `proxies` key of a `chain` must contain an array of proxy names declared as keys in the `proxies` section.
As mentionned in the previous paragraph, for each proxy declared in `proxies` section, an implicit
chain (see next paragraph) is created with the same name. It has defaults parameters and is 
composed of the single associated proxy.



### Routes

The built-in configuration mode for routing is through the configuration file. It associates
addresses with chain names. The file must contain a map of routing tables. Map keys
are chosen freely but must match the ones used in the `servers` section. 
Each routing table contains a `default` key representing the default route and a `blocks` key
which is an array of rule blocks. Each
rule block contains a `comment`, a set of `rules`, and an associated chain
name. Rules are evaluated: given an address in the `host:port` format, they can
be `true` or `false`. For a given address, blocks are evaluated in their
declaration order. Blocks can be disabled by setting the `disable` field to `true`.
This allows for a form of "commenting", which is not possible in JSON.
The evaluation stops at the first block that is `true` and
the associated chain name is returned. Each opened server (from `servers` section)
is associated with one routing table from the configuration. Requests received on 
each server are routed according to the matching routing table. If all blocks evaluate to 
`false` the default route is used. If `default` is not defined, connections are dropped by default.


Block fields:
 - `comment` (string)
 - `rules` (Rule or RuleCombo)
 - `route` (string)
 - `disable` (bool)

Rule fields: 
 - `rule` (string): rule type, `regexp`, `subnet`.
 - `variable` (string): variable for regexp evaluation, `host`, `port` or `addr` (host:port).
 - `content` (string): content of the rule, depends on the rule type (see below).
 - `negate` (bool) [optional]: whether to negate the rule.

RuleCombo fields:
 - `rule1` (Rule or RuleCombo): left operand.
 - `op` (string): operator, `AND`, `And`, `and`, `&`, `&&`, `OR`, `Or`, `or`, `|`, `||`.
 - `rule2` (Rule or RuleCombo): right operand.

Rule types:
 - `regexp`: match the variable defined in `variable` (`host`, `port` or `addr=host:port`) against the regexp in `content`.
 - `subnet`: checks if host is in the subnet defined in `content`. If host is a domain name and not a subnet address, the rule returns false.

The rule blocks from `routes` section or the PAC function must return declared
chain names, not proxy names. If you want to use a single proxy, you must wrap
it in a chain. The `drop` name is special and does not need to be declared in
this configuration. If the PAC function or a routing block returns `drop` as a
chain name, then the connection is dropped.

If bbs is built with PAC support and `-pac` arguments points to a PAC file, routes
defined in the configuration file will not be used. PAC file routing does not support
multiple routing tables. The same PAC file will be used for every opened server.


### Servers

The listeners opened by bbs must be declared in the `servers` section as a list of 
connection strings of format `protocol://bind_addr:bind_port:routing_table` or 
`protocol://bind_addr:bind_port:chain:dest_addr:dest_port`.

- `protocol` can be `http` or `socks5` if the `routing_table` is provided
- `protocol` can be `fwd` if `chain`, `dest_addr` and `dest_port` are provided
- `routing_table` must match one of the tables defined in `routes` section
- `chain` must match one of the chains defined in `chains` section


### Hosts

Custom host resolution (similar to `/etc/hosts`) can be configured in the
`hosts` section as a map of strings. Map keys correspond to the hostname
and the values to the IP address the host should resolve to.

It should be noted that map keys may also be IP addresses. In this case the 
key IP address will be replaced by the value IP address. Similarly, map values
may be hostnames and will replace the corresponding map key.

If defined, custom host resolutions occur at the beginning of the connection phase, 
after the routing decision is made and before any local DNS resolution (if the chain 
is configured with proxyDns=false) and before sending the destination address to the
 various proxies of the chain.

### PAC script

If `bbs` is built with PAC support, routing can be configured with a PAC script
instead of a JSON configuration file. However, this requires using an untrusted
Go library. The PAC file path must be provided with `-pac`. 

The PAC script must define the `FindProxyForURL(url, host)` function. The
values returned by this function must match the names of the chains (not the
proxies) declared in the JSON configuration. 
