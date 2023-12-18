# BBS

## Description

`bbs` is a router for SOCKS and HTTP proxies. It exposes a SOCKS5 (or HTTP
CONNECT) service and forwards incoming requests to proxies or chains of proxies
based on the request's target. Routing can be configured with a PAC script (if
built with PAC support), or through a JSON file.

## Build 

Clone the repository, then: 

```bash
cd bbs
go mod init bbs
go mod tidy
go build
```

To build bbs with PAC script support: 
```bash
go build -tags pac
```

Note: PAC relies on unaudited third-party libraries.


## Configuration

Configuration is performed through 2 files:

- Routing rules (PAC script or JSON file)
- Proxies and chains parameters (JSON file)

`bbs` reloads configuration files on SIGHUP, use `kill -HUP <pid>` to reload.


### Proxies and chains JSON configuration

Upstream proxies and chains must be declared in a JSON configuration file
(`bbs.json` by default, or `-c <path>`). The file should follow the structure
from the provided example.

For proxies:

- `prot`, `host` and `port` are required
- `user` and `pass` are optional
- `prot` can be `socks5` or `httpconnect`

Chains have proxychains-like parameters (cf.
https://github.com/rofl0r/proxychains-ng). The `proxies` key of a `chain` must
contain an array of proxy names declared as keys in the `proxies` section.

The `routing` blocks of the JSON file or the PAC function must return declared
chain names, not proxy names. If you want to use a single proxy, you must wrap
it in a chain. The `drop` name is special and does not need to be declared in
this configuration. If the PAC function or a routing block returns `drop` as a
chain name, then the connection is dropped.

Configuration example: 

```json
{
  "proxies": {
    "proxy1": {
      "prot": "socks5",
      "host": "127.0.0.1",
      "port": "1337"
    },
    "proxy2": {
      "prot": "socks5",
      "host": "127.0.0.1",
      "port": "1338"
    },
    "proxy3": {
      "prot": "httpconnect",
      "host": "127.0.0.1",
      "port": "8080",
      "user": "foo",
      "pass": "bar"
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
    "chain2": {
      "proxies": [
        "proxy1"
      ]
    },
    "direct": {
      "proxies": []
    }
  }
}
```

### Routing JSON configuration

The built-in configuration mode for routing is a JSON file. It associates
addresses with chain names. The file must contain an array of rule blocks. Each
rule block contains a `comment`, a set of `rules`, and an associated chain
name. Rules are evaluated: given an address in the `host:port` format, they can
be `true` or `false`. For a given address, blocks are evaluated in their
declaration order. The evaluation stops at the first block that is `true` and
the associated chain name is returned.

Here is an example configuration:

```json
[
  {
    "comment": "Block1 comment",
    "rules": {
      "rule": "regexp",
      "variable": "host",
      "content": "me\\.gandi\\.net"
    },
    "route": "chain2"
  },
  {
    "comment": "Route web traffic towards 10.35.0.0/16 through chain1",
    "rules": {
      "rule1": {
        "rule": "subnet",
        "content": "10.35.0.0/16"
      },
      "op": "AND",
      "rule2": {
        "rule": "regexp",
        "variable": "port",
        "content": "^(80|443)$"
      }
    },
    "route": "chain1"
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
    "comment": "Default routing through direct chain",
    "rules": {
      "rule": "true"
    },
    "route": "direct"
  }
]
```

Block fields:
 - `comment` (string)
 - `rules` (Rule or RuleCombo)
 - `route` (string)

Rule fields: 
 - `rule` (string): rule type, `regexp`, `subnet` or `true`.
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
 - `true`: returns `true` for every address. Useful for default routing at the end of the block array.

The path of the routing configuration can be set with the `-routes` flag. If
bbs is built without PAC support, `-routes` default value is `routes.json`. If
bbs is built with PAC support, `-routes` has no default value and must be
explicitly defined in order to use a JSON file for routing (note that with PAC
support, `-routes` and `-pac` are mutually exclusive and collectively
exhaustive)


### PAC script

If `bbs` is built with PAC support, routing can be configured with a PAC script
instead of a JSON configuration file. However, this requires using an untrusted
Go library. The PAC file path must be provided with `-pac`. At least one of
`-pac` and `-routes` argument (but not both) must be provided.

The PAC script must define the `FindProxyForURL(url, host)` function. The
values returned by this function must match the names of the chains (not the
proxies) declared in the JSON configuration. 


### Custom host resolution

Custom host resolution (similar to `/etc/hosts`) can be configured in a JSON
file. The path to the file must be passed in `-custom-hosts`. The file must
have the following format: 

```json
{
  "host1.domain.com": "10.0.0.1",
  "host2": "192.0.0.1",
  "host3": "127.0.0.1"
}
```
