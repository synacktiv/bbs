#!/usr/bin/python3

import json
import argparse
import os
import tempfile
import re
import ipaddress
import pyparsing as pp


class Config:
    """JSON Configuration file wrapper"""

    # --- Top Level Keys ---
    GLOBAL_KEY_PROXIES = "proxies"
    GLOBAL_KEY_CHAINS = "chains"
    GLOBAL_KEY_ROUTES = "routes" 
    GLOBAL_KEY_SERVERS = "servers"
    GLOBAL_KEY_HOSTS = "hosts"

    # --- Proxy Keys ---
    PROXY_KEY_CONNSTRING = "connstring"
    PROXY_KEY_USER = "user"
    PROXY_KEY_PASS = "pass"

    # --- Chain Keys ---
    CHAIN_KEY_PROXYDNS = "proxyDns"
    CHAIN_KEY_TCPREADTIMEOUT = "tcpReadTimeout"
    CHAIN_KEY_TCPCONNECTTIMEOUT = "tcpConnectTimeout"
    CHAIN_KEY_PROXIES = "proxies"

    # --- Table Keys --- 
    TABLE_KEY_DEFAULT = "default" # Default route for the table
    TABLE_KEY_BLOCKS = "blocks" # List of route blocks in the table

    # --- Route Block Keys (Element in the list for a table) ---
    ROUTEBLOCK_KEY_COMMENT = "comment"
    ROUTEBLOCK_KEY_RULES = "rules" # This holds the parsed rule structure (dict)
    ROUTEBLOCK_KEY_ROUTE = "route" # Target chain name or "drop"
    ROUTEBLOCK_KEY_DISABLE = "disable"

    # --- Route Rule Keys (Used within the 'rules' dict) ---
    ROUTERULE_KEY_RULE = "rule" # Type of rule: regexp, subnet, true, rulecombo
    ROUTERULE_KEY_VARIABLE = "variable" # For regexp: host, port, addr
    ROUTERULE_KEY_CONTENT = "content"   # For regexp/subnet: the pattern/CIDR
    ROUTERULE_KEY_NEGATE = "negate"     # Boolean

    # --- Route Rule Combo Keys (Used when rule="rulecombo") ---
    ROUTERULECOMBO_KEY_RULE1 = "rule1" # Nested rule dict
    ROUTERULECOMBO_KEY_RULE2 = "rule2" # Nested rule dict
    ROUTERULECOMBO_KEY_OP = "op"     # "and" or "or"

    # --- Route Rule Types ---
    ROUTERULE_TYPE_REGEX = "regexp"
    ROUTERULE_TYPE_SUBNET = "subnet"
    ROUTERULE_TYPE_TRUE = "true" # Represents an always-true condition
    ROUTERULE_TYPE_COMBO = "rulecombo" # Represents combined rules

    # --- Route Targets ---
    ROUTERULE_DROP = "drop"

    contents = dict()

    def __init__(self, path):
        self.path = os.path.abspath(path)
        self.load()

    def load(self):
        """Load JSON config from file"""
        try:
            with open(self.path) as file:
                self.contents = json.load(file)
            print(f"Config loaded from {self.path}")
        except json.JSONDecodeError as e:
            print(f"Error decoding JSON from {self.path}: {e}")
            self.contents = {} # Start fresh if decode fails
        except FileNotFoundError:
            print(f"Config file {self.path} not found. Creating default structure.")
            self.contents = {} # Start fresh if not found
        except Exception as e:
            print(f"Unexpected error loading config {self.path}: {e}")
            self.contents = {}

        # Ensure default structure exists
        self.contents.setdefault(self.GLOBAL_KEY_PROXIES, {})
        self.contents.setdefault(self.GLOBAL_KEY_CHAINS, {})
        self.contents.setdefault(self.GLOBAL_KEY_ROUTES, {})
        self.contents.setdefault(self.GLOBAL_KEY_SERVERS, [])
        self.contents.setdefault(self.GLOBAL_KEY_HOSTS, {})
        # No initial save here, let operations trigger saves

    def save(self):
        """Save JSON config to file atomically"""
        try:
            directory = os.path.dirname(self.path)
            basename = os.path.basename(self.path)
            # Ensure directory exists
            if not os.path.exists(directory):
                os.makedirs(directory, exist_ok=True)
                print(f"Created directory {directory}")

            fd, tmp = tempfile.mkstemp(prefix=f'.{basename}.', dir=directory)
            with os.fdopen(fd, 'w') as file:
                json.dump(self.contents, file, indent=4) # Pretty print
                file.flush()
                os.fsync(fd) # Ensure data is written to disk
            os.rename(tmp, self.path)
            print(f"Config saved to {self.path}")
        except Exception as e:
            print(f"Error saving config to {self.path}: {e}")



    # --- Proxy Management ---
    def _get_unused_proxy_name(self):
        names = self.contents[self.GLOBAL_KEY_PROXIES].keys()
        for i in range(1, 1000):
            name = f"proxy{i}"
            if name not in names:
                return name
        raise ValueError("Could not find an unused proxy name (tried up to proxy999)")

    def get_proxy(self, name):
        return self.contents[self.GLOBAL_KEY_PROXIES].get(name, None)

    def get_proxies(self):
        return self.contents[self.GLOBAL_KEY_PROXIES]

    def add_proxy(self, name, connstring, user=None, password=None):
        if name in self.contents[self.GLOBAL_KEY_PROXIES]:
            print(f"Error: Proxy '{name}' already exists.")
            return False
        self.contents[self.GLOBAL_KEY_PROXIES][name] = {
            self.PROXY_KEY_CONNSTRING: connstring
        }
        if user:
            self.contents[self.GLOBAL_KEY_PROXIES][name][self.PROXY_KEY_USER] = user
        if password:
            self.contents[self.GLOBAL_KEY_PROXIES][name][self.PROXY_KEY_PASS] = password
        self.save()
        print(f"Proxy '{name}' added.")
        return True

    def delete_proxy(self, name=None):
        if name is None: # Delete all
            if not self.contents[self.GLOBAL_KEY_PROXIES]:
                print("No proxies to delete.")
                return False
            self.contents[self.GLOBAL_KEY_PROXIES] = {}
            self.save()
            print("All proxies deleted.")
            return True
        else:
            if name not in self.contents[self.GLOBAL_KEY_PROXIES]:
                print(f"Error: Proxy '{name}' does not exist.")
                return False
            del self.contents[self.GLOBAL_KEY_PROXIES][name]
            self.save()
            print(f"Proxy '{name}' deleted.")
            return True

    def update_proxy(self, name, protocol=None, host=None, port=None, user=None, password=None, newName=None):
        if name not in self.contents[self.GLOBAL_KEY_PROXIES]:
            print(f"Error: Proxy '{name}' does not exist.")
            return False

        proxy_data = self.contents[self.GLOBAL_KEY_PROXIES][name]

        # Update user/pass directly
        if user is not None: # Allow empty string for user
            proxy_data[self.PROXY_KEY_USER] = user
        if password is not None: # Allow empty string for password
            proxy_data[self.PROXY_KEY_PASS] = password

        # Update connstring components
        oldConnstring = proxy_data.get(self.PROXY_KEY_CONNSTRING, "://:")
        try:
            parts = oldConnstring.split("://", 1)
            oldProtocol = parts[0] if len(parts) > 1 else ""
            host_port = parts[1] if len(parts) > 1 else ""
            host_port_parts = host_port.split(":", 1)
            oldHost = host_port_parts[0] if len(host_port_parts) > 0 else ""
            oldPort = host_port_parts[1] if len(host_port_parts) > 1 else ""
        except Exception:
            print(f"Warning: Could not parse existing connstring '{oldConnstring}' for proxy '{name}'. Updating based on provided values.")
            oldProtocol, oldHost, oldPort = "", "", ""

        newProtocol = protocol if protocol is not None else oldProtocol
        newHost = host if host is not None else oldHost
        newPort = port if port is not None else oldPort

        proxy_data[self.PROXY_KEY_CONNSTRING] = f"{newProtocol}://{newHost}:{newPort}"

        # Handle renaming
        if newName and newName != name:
            if newName in self.contents[self.GLOBAL_KEY_PROXIES]:
                print(f"Error: Cannot rename proxy to '{newName}', name already exists.")
                # Revert changes before renaming attempt? Or save intermediate? Let's save intermediate.
                self.contents[self.GLOBAL_KEY_PROXIES][name] = proxy_data # Put potentially modified data back
                self.save()
                return False
            else:
                self.contents[self.GLOBAL_KEY_PROXIES][newName] = self.contents[self.GLOBAL_KEY_PROXIES].pop(name)
                print(f"Proxy '{name}' updated and renamed to '{newName}'.")
        else:
             self.contents[self.GLOBAL_KEY_PROXIES][name] = proxy_data
             print(f"Proxy '{name}' updated.")

        self.save()
        return True

    # --- Chain Management ---
    def _get_unused_chain_name(self):
        names = self.contents[self.GLOBAL_KEY_CHAINS].keys()
        for i in range(1, 1000):
            name = f"chain{i}"
            if name not in names:
                return name
        raise ValueError("Could not find an unused chain name (tried up to chain999)")

    def get_chain(self, name):
        return self.contents[self.GLOBAL_KEY_CHAINS].get(name, None)

    def get_chains(self):
        return self.contents[self.GLOBAL_KEY_CHAINS]

    def add_chain(self, name, proxies, tcpReadTimeout=None, tcpConnectTimeout=None, proxyDns=None):
        if name in self.contents[self.GLOBAL_KEY_CHAINS]:
            print(f"Error: Chain '{name}' already exists.")
            return False

        # Validate proxies exist
        for proxy_name in proxies:
            if proxy_name not in self.contents[self.GLOBAL_KEY_PROXIES]:
                print(f"Error: Proxy '{proxy_name}' specified in chain '{name}' does not exist.")
                return False

        chain_data = {self.CHAIN_KEY_PROXIES: proxies}
        if tcpReadTimeout is not None:
            try:
                chain_data[self.CHAIN_KEY_TCPREADTIMEOUT] = int(tcpReadTimeout)
            except ValueError:
                print(f"Error: tcpReadTimeout '{tcpReadTimeout}' must be an integer.")
                return False
        if tcpConnectTimeout is not None:
            try:
                chain_data[self.CHAIN_KEY_TCPCONNECTTIMEOUT] = int(tcpConnectTimeout)
            except ValueError:
                print(f"Error: tcpConnectTimeout '{tcpConnectTimeout}' must be an integer.")
                return False
        if proxyDns is not None:
            chain_data[self.CHAIN_KEY_PROXYDNS] = bool(proxyDns)

        self.contents[self.GLOBAL_KEY_CHAINS][name] = chain_data
        self.save()
        print(f"Chain '{name}' added.")
        return True

    def delete_chain(self, name=None):
        if name is None: # Delete all
            if not self.contents[self.GLOBAL_KEY_CHAINS]:
                print("No chains to delete.")
                return False
            self.contents[self.GLOBAL_KEY_CHAINS] = {}
            self.save()
            print("All chains deleted.")
            return True
        else:
            if name not in self.contents[self.GLOBAL_KEY_CHAINS]:
                print(f"Error: Chain '{name}' does not exist.")
                return False
            del self.contents[self.GLOBAL_KEY_CHAINS][name]
            # TODO: Check if chain is used in routes and warn/prevent?
            self.save()
            print(f"Chain '{name}' deleted.")
            return True

    def update_chain(self, name, proxies=None, tcpReadTimeout=None, tcpConnectTimeout=None, proxyDns=None, newName=None):
        if name not in self.contents[self.GLOBAL_KEY_CHAINS]:
            print(f"Error: Chain '{name}' does not exist.")
            return False

        chain_data = self.contents[self.GLOBAL_KEY_CHAINS][name]

        if proxies:
            # Validate new proxies exist
            for proxy_name in proxies:
                if proxy_name not in self.contents[self.GLOBAL_KEY_PROXIES]:
                    print(f"Error: Proxy '{proxy_name}' specified for chain '{name}' does not exist.")
                    return False
            chain_data[self.CHAIN_KEY_PROXIES] = proxies

        if tcpReadTimeout is not None:
            try:
                chain_data[self.CHAIN_KEY_TCPREADTIMEOUT] = int(tcpReadTimeout)
            except ValueError:
                print(f"Error: tcpReadTimeout '{tcpReadTimeout}' must be an integer.")
                return False
        if tcpConnectTimeout is not None:
             try:
                chain_data[self.CHAIN_KEY_TCPCONNECTTIMEOUT] = int(tcpConnectTimeout)
             except ValueError:
                print(f"Error: tcpConnectTimeout '{tcpConnectTimeout}' must be an integer.")
                return False
        if proxyDns is not None:
            chain_data[self.CHAIN_KEY_PROXYDNS] = bool(proxyDns)

        # Handle renaming
        if newName and newName != name:
            if newName in self.contents[self.GLOBAL_KEY_CHAINS]:
                print(f"Error: Cannot rename chain to '{newName}', name already exists.")
                self.contents[self.GLOBAL_KEY_CHAINS][name] = chain_data # Put potentially modified data back
                self.save() # Save intermediate state
                return False
            else:
                # TODO: Update routes using this chain? Difficult without back-refs. Warn user.
                print(f"Warning: Renaming chain '{name}' to '{newName}'. Routes using the old name might need manual updates.")
                self.contents[self.GLOBAL_KEY_CHAINS][newName] = self.contents[self.GLOBAL_KEY_CHAINS].pop(name)
                print(f"Chain '{name}' updated and renamed to '{newName}'.")
        else:
            self.contents[self.GLOBAL_KEY_CHAINS][name] = chain_data
            print(f"Chain '{name}' updated.")

        self.save()
        return True


    # --- Server Management ---
    def get_server(self, index):
        try:
            index = int(index)
            return self.contents[self.GLOBAL_KEY_SERVERS][index]
        except (ValueError, IndexError):
            print(f"Error: Invalid server index {index}")
            return None

    def get_servers(self):
        return self.contents[self.GLOBAL_KEY_SERVERS]

    def add_server(self, protocol, host, port, table):
        connstring = f"{protocol}://{host}:{port}:{table}"
        # Check if server already exists (exact match)
        if connstring in self.contents[self.GLOBAL_KEY_SERVERS]:
            print(f"Error: Server '{connstring}' already exists.")
            return False
        # Check if table exists in routes
        if table not in self.contents[self.GLOBAL_KEY_ROUTES]:
             print(f"Warning: Routing table '{table}' referenced by server does not exist yet. Create it using 'route add'.")
             # Allow adding server anyway, but warn.

        self.contents[self.GLOBAL_KEY_SERVERS].append(connstring)
        self.save()
        print(f"Server '{connstring}' added at index {len(self.contents[self.GLOBAL_KEY_SERVERS]) - 1}.")
        return True
    
    def add_server_fwd(self, local_host, local_port, chain, remote_host, remote_port):
        """Adds a forwarder server with the format: fwd://local_host:local_port:chain:remote_host:remote_port"""
        connstring = f"fwd://{local_host}:{local_port}:{chain}:{remote_host}:{remote_port}"
        # Check if server already exists (exact match)
        if connstring in self.contents[self.GLOBAL_KEY_SERVERS]:
            print(f"Error: Forwarder '{connstring}' already exists.")
            return False
        # Check if chain exists
        if not self.is_route_valid(chain):
            print(f"Error: Chain '{chain}' does not exist. Create it first using 'chain add'.")
            return False

        self.contents[self.GLOBAL_KEY_SERVERS].append(connstring)
        self.save()
        print(f"Forwarder '{connstring}' added at index {len(self.contents[self.GLOBAL_KEY_SERVERS]) - 1}.")
        return True

    def delete_server(self, index=None):
        if index is None: # Delete all
            if not self.contents[self.GLOBAL_KEY_SERVERS]:
                print("No servers to delete.")
                return False
            self.contents[self.GLOBAL_KEY_SERVERS] = []
            self.save()
            print("All servers deleted.")
            return True

        try:
            index = int(index)
            if 0 <= index < len(self.contents[self.GLOBAL_KEY_SERVERS]):
                deleted_server = self.contents[self.GLOBAL_KEY_SERVERS].pop(index)
                self.save()
                print(f"Server '{deleted_server}' at index {index} deleted.")
                return True
            else:
                print(f"Error: Invalid server index {index}.")
                return False
        except ValueError:
            print(f"Error: Server index '{index}' must be an integer.")
            return False


    def update_server(self, index, protocol=None, host=None, port=None, table=None):
        try:
            index = int(index)
            if not (0 <= index < len(self.contents[self.GLOBAL_KEY_SERVERS])):
                print(f"Error: Invalid server index {index}.")
                return False

            server = self.contents[self.GLOBAL_KEY_SERVERS][index]
            try:
                parts = server.split("://", 1)
                oldProtocol = parts[0]
                host_port_table = parts[1].split(":", 2)
                oldHost = host_port_table[0]
                oldPort = host_port_table[1]
                oldTable = host_port_table[2]
            except Exception:
                 print(f"Warning: Could not parse existing server string '{server}'. Updating based on provided values.")
                 oldProtocol, oldHost, oldPort, oldTable = "", "", "", ""

            newProtocol = protocol if protocol is not None else oldProtocol
            newHost = host if host is not None else oldHost
            newPort = port if port is not None else oldPort
            newTable = table if table is not None else oldTable

            # Check if new table exists
            if newTable not in self.contents[self.GLOBAL_KEY_ROUTES]:
                 print(f"Warning: New routing table '{newTable}' referenced by server does not exist yet. Create it using 'route add'.")

            new_connstring = f"{newProtocol}://{newHost}:{newPort}:{newTable}"
            self.contents[self.GLOBAL_KEY_SERVERS][index] = new_connstring
            self.save()
            print(f"Server at index {index} updated to '{new_connstring}'.")
            return True

        except ValueError:
            print(f"Error: Server index '{index}' must be an integer.")
            return False

    # --- Host Management ---
    def get_host(self, name):
        return self.contents[self.GLOBAL_KEY_HOSTS].get(name, None)

    def get_hosts(self):
        return self.contents[self.GLOBAL_KEY_HOSTS]

    def add_host(self, name, ip):
        if name in self.contents[self.GLOBAL_KEY_HOSTS]:
            print(f"Error: Host '{name}' already exists.")
            return False
        # Basic IP validation (optional, could be more robust)
        try:
            ipaddress.ip_address(ip)
        except ValueError:
             print(f"Warning: '{ip}' does not appear to be a valid IP address. Adding anyway.")
        self.contents[self.GLOBAL_KEY_HOSTS][name] = ip
        self.save()
        print(f"Host '{name}' added with IP '{ip}'.")
        return True

    def delete_host(self, name=None):
        if name is None: # Delete all
             if not self.contents[self.GLOBAL_KEY_HOSTS]:
                print("No hosts to delete.")
                return False
             self.contents[self.GLOBAL_KEY_HOSTS] = {}
             self.save()
             print("All hosts deleted.")
             return True
        else:
            if name not in self.contents[self.GLOBAL_KEY_HOSTS]:
                print(f"Error: Host '{name}' does not exist.")
                return False
            del self.contents[self.GLOBAL_KEY_HOSTS][name]
            self.save()
            print(f"Host '{name}' deleted.")
            return True

    def update_host(self, name, ip=None, newName=None):
        if name not in self.contents[self.GLOBAL_KEY_HOSTS]:
            print(f"Error: Host '{name}' does not exist.")
            return False

        if ip:
            # Validate new IP address
            try:
                ipaddress.ip_address(ip)
            except ValueError:
                print(f"Error: '{ip}' does not appear to be a valid IP address.")
                return False
            self.contents[self.GLOBAL_KEY_HOSTS][name] = ip
            print(f"Host '{name}' updated to IP '{ip}'.")


        if newName and newName != name:
            if newName in self.contents[self.GLOBAL_KEY_HOSTS]:
                print(f"Error: Cannot rename host to '{newName}', name already exists.")
                return False
            self.contents[self.GLOBAL_KEY_HOSTS][newName] = self.contents[self.GLOBAL_KEY_HOSTS].pop(name)
            print(f"Renamed '{name}' to '{newName}'.")

        self.save()
        return True

    # --- Route Management ---
    def get_routing_tables(self):
        """Returns a list of routing table names."""
        return list(self.contents[self.GLOBAL_KEY_ROUTES].keys())
    
    def is_route_valid(self, route_target):
        """Checks if a route target is valid (either 'drop' or an existing chain (or implicit chain based on proxy name))."""
        return route_target == self.ROUTERULE_DROP or route_target in self.contents[self.GLOBAL_KEY_CHAINS] or route_target in self.contents[self.GLOBAL_KEY_PROXIES]
    
    def update_default_route(self, table_name, new_default):
        """Updates the default route for a given table."""
        if table_name not in self.contents[self.GLOBAL_KEY_ROUTES]:
            print(f"Error: Routing table '{table_name}' does not exist.")
            return False

        # Validate the new default route (must be 'drop' or an existing chain)
        if not self.is_route_valid(new_default):
            print(f"Error: Default route '{new_default}' is invalid. Use 'drop' or an existing chain name.")
            return False

        self.contents[self.GLOBAL_KEY_ROUTES][table_name][self.TABLE_KEY_DEFAULT] = new_default
        self.save()
        print(f"Default route for table '{table_name}' updated to '{new_default}'.")
        return True

    def get_routes(self, table_name):
        """Returns the list of route blocks for a given table."""
        if table_name not in self.contents[self.GLOBAL_KEY_ROUTES]:
            # print(f"Error: Routing table '{table_name}' does not exist.")
            return None
        return self.contents[self.GLOBAL_KEY_ROUTES][table_name]

    def add_route(self, table_name, rule_dict, route_target, comment=None, position=None, disable=False):
        """Adds a new route block to a table."""
        # Ensure table exists
        if table_name not in self.contents[self.GLOBAL_KEY_ROUTES]:
            self.contents[self.GLOBAL_KEY_ROUTES][table_name] = {self.TABLE_KEY_DEFAULT: self.ROUTERULE_DROP, self.TABLE_KEY_BLOCKS: []}
            print(f"Routing table '{table_name}' created with default route 'drop'.")

        table = self.contents[self.GLOBAL_KEY_ROUTES][table_name]
        table_blocks = table.setdefault(self.TABLE_KEY_BLOCKS, [])

        # Validate route target (must be a chain or 'drop')
        if not self.is_route_valid(route_target):
            print(f"Error: Route target chain '{route_target}' does not exist. Use 'drop' or an existing chain name.")
            return False

        route_block = {
            self.ROUTEBLOCK_KEY_RULES: rule_dict,
            self.ROUTEBLOCK_KEY_ROUTE: route_target,
            self.ROUTEBLOCK_KEY_DISABLE: bool(disable)
        }
        if comment:
            route_block[self.ROUTEBLOCK_KEY_COMMENT] = comment


        if position is None:
            # Add to the end
            table_blocks.append(route_block)
            print(f"Route added to table '{table_name}' at the end (index {len(table_blocks) - 1}).")
        else:
            try:
                pos = int(position)
                if 0 <= pos <= len(table_blocks): # Allow inserting at the very end
                    table_blocks.insert(pos, route_block)
                    print(f"Route inserted into table '{table_name}' at index {pos}.")
                else:
                     print(f"Error: Invalid position {pos}. Must be between 0 and {len(table_blocks)}.")
                     return False
            except ValueError:
                print(f"Error: Position '{position}' must be an integer.")
                return False

        self.save()
        return True
    
    def update_route(self, table_name, index, rule_dict=None, route_target=None, comment=None, disable=None, new_index=None):
        """Updates an existing route block in a table by index, including moving it to a new position."""
        if table_name not in self.contents[self.GLOBAL_KEY_ROUTES]:
            print(f"Error: Routing table '{table_name}' does not exist.")
            return False

        table = self.contents[self.GLOBAL_KEY_ROUTES][table_name]
        table_blocks = table.get(self.TABLE_KEY_BLOCKS, [])

        try:
            idx = int(index)
            if 0 <= idx < len(table_blocks):
                route_block = table_blocks.pop(idx)  # Remove the rule from its current position

                # Update fields if provided
                if rule_dict is not None:
                    route_block[self.ROUTEBLOCK_KEY_RULES] = rule_dict
                if route_target is not None:
                    # Validate route target (must be a chain or 'drop')
                    if not self.is_route_valid(route_target):
                        print(f"Error: Route target chain '{route_target}' does not exist. Use 'drop' or an existing chain name.")
                        return False
                    route_block[self.ROUTEBLOCK_KEY_ROUTE] = route_target
                if comment is not None:
                    route_block[self.ROUTEBLOCK_KEY_COMMENT] = comment
                if disable is not None:
                    route_block[self.ROUTEBLOCK_KEY_DISABLE] = bool(disable)

                # Handle moving the rule to a new index
                if new_index is not None:
                    try:
                        new_idx = int(new_index)
                        if 0 <= new_idx <= len(table_blocks):  # Allow inserting at the end
                            table_blocks.insert(new_idx, route_block)
                            print(f"Route moved from index {idx} to {new_idx} in table '{table_name}'.")
                        else:
                            print(f"Error: Invalid new index {new_idx}. Must be between 0 and {len(table_blocks)}.")
                            return False
                    except ValueError:
                        print(f"Error: New index '{new_index}' must be an integer.")
                        return False
                else:
                    # If no new index is provided, reinsert the rule at its original position
                    table_blocks.insert(idx, route_block)

                print(f"Route at index {idx} in table '{table_name}' updated.")
                self.save()
                return True
            else:
                print(f"Error: Invalid index {idx}. Must be between 0 and {len(table_blocks) - 1}.")
                return False
        except ValueError:
            print(f"Error: Index '{index}' must be an integer.")
            return False

    def delete_route(self, table_name, index):
        """Deletes a route block from a table by index."""
        if table_name not in self.contents[self.GLOBAL_KEY_ROUTES]:
            print(f"Error: Routing table '{table_name}' does not exist.")
            return False

        table = self.contents[self.GLOBAL_KEY_ROUTES][table_name]
        table_blocks = table.get(self.TABLE_KEY_BLOCKS, [])

        try:
            idx = int(index)
            if 0 <= idx < len(table_blocks):
                deleted_route = table_blocks.pop(idx)
                # If table becomes empty, should we delete the table? Let's keep it for now.
                # if not table_routes:
                #     del self.contents[self.GLOBAL_KEY_ROUTES][table_name]
                #     print(f"Route at index {idx} deleted. Table '{table_name}' is now empty and removed.")
                # else:
                print(f"Route at index {idx} deleted from table '{table_name}'.")
                self.save()
                return True
            else:
                print(f"Error: Invalid index {idx}. Must be between 0 and {len(table_blocks) - 1}.")
                return False
        except ValueError:
            print(f"Error: Index '{index}' must be an integer.")
            return False

    def delete_routing_table(self, table_name):
        """Deletes an entire routing table."""
        if table_name not in self.contents[self.GLOBAL_KEY_ROUTES]:
            print(f"Error: Routing table '{table_name}' does not exist.")
            return False

        # Check if table is used by any server
        used_by_servers = []
        for i, server_str in enumerate(self.contents[self.GLOBAL_KEY_SERVERS]):
             try:
                 if server_str.endswith(f":{table_name}"):
                     used_by_servers.append(f"Server index {i} ('{server_str}')")
             except Exception:
                 pass # Ignore malformed server strings

        if used_by_servers:
            print(f"Error: Cannot delete table '{table_name}' because it is used by:")
            for usage in used_by_servers:
                print(f"  - {usage}")
            print("Update or delete these servers first.")
            return False

        del self.contents[self.GLOBAL_KEY_ROUTES][table_name]
        print(f"Routing table '{table_name}' deleted.")
        self.save()
        return True


# --- Rule Expression Parser ---

class RuleParser:
    """Parses rule expressions into BBS JSON rule structure."""

    def __init__(self, config):
        self.config = config # To access constants
        self._parser = self._build_parser()

    def _build_parser(self):
        """Creates and returns the pyparsing grammar object."""

        # --- Parse Actions (Handlers) ---
        # These functions are called when a grammar rule is successfully matched.
        # They are responsible for transforming the parsed tokens into the desired dictionary structure.

        def handle_simple_rule(tokens):
            """
            Handles a simple rule like 'host is example.com' or 'not port is 80'.
            It constructs the base dictionary for a single rule.
            """
            # The Group puts all tokens for this rule into a list.
            rule_tokens = tokens[0]
            
            negated = False
            if rule_tokens[0].lower() == 'not':
                negated = True
                rule_tokens = rule_tokens[1:]  # Remove 'not' from the list
            
            variable, operator, value = rule_tokens
            variable = variable.lower()
            operator = operator.lower()
            
            result_dict = {}

            # Logic to determine the JSON "rule" type and content based on the expression
            if variable == 'host' and operator == 'in':
                result_dict = {"rule": "subnet", "content": value}
            elif variable == 'host' and operator == 'is':
                # Check if the value is an IPv4 address to apply the /32 subnet rule
                if re.match(r"^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$", value):
                    result_dict = {"rule": "subnet", "content": f"{value}/32"}
                else:  # Otherwise, treat it as a domain for a regexp rule
                    result_dict = {"rule": "regexp", "variable": "host", "content": f"^{re.escape(value)}$"}
            elif operator == 'is':  # For 'port is ...' and 'addr is ...'
                result_dict = {"rule": "regexp", "variable": variable, "content": f"^{re.escape(value)}$"}
            elif operator == 'like':  # For '... like "..."'
                result_dict = {"rule": "regexp", "variable": variable, "content": value}

            if negated:
                result_dict["negate"] = "true"
                
            return result_dict

        def handle_binary_op(tokens):
            print(tokens)
            """
            Handles logical combinations ('and', 'or').
            It takes a sequence of operands and operators and nests them correctly.
            """
            # The tokens are provided in a nested list, e.g., [[operand1, operator, operand2]]
            t = tokens[0]
            
            # Start with the leftmost operand
            result_dict = t[0]
            
            # Sequentially apply the operators to create nested structures
            # e.g., A and B and C -> ((A and B) and C)
            for i in range(1, len(t), 2):
                op = t[i].lower()
                right_operand = t[i+1]
                result_dict = {"rule1": result_dict, "op": op, "rule2": right_operand}
                
            return result_dict

        # --- Grammar Definition ---

        # Use packrat for better performance on complex grammars
        pp.ParserElement.enablePackrat()

        # Define keywords and suppress them from the output (we handle them in parse actions)
        LPAR, RPAR = map(pp.Suppress, "()")
        NOT = pp.Keyword("not", caseless=True)
        AND = pp.Keyword("and", caseless=True)
        OR = pp.Keyword("or", caseless=True)
        IN = pp.Keyword("in", caseless=True)
        IS = pp.Keyword("is", caseless=True)
        LIKE = pp.Keyword("like", caseless=True)

        # Define the terminals of the language
        variable = pp.oneOf("host port addr", caseless=True)
        operator = IN | IS | LIKE
        # A value can be a quoted string (for regexes) or any other word without parentheses
        value = pp.QuotedString(quoteChar='"', unquoteResults=True) | pp.QuotedString(quoteChar="'", unquoteResults=True) | pp.Word(pp.printables, excludeChars="()")

        # Define the base element of our grammar: a single expression.
        # We group it so the parse action gets all its tokens together.
        simple_expr = pp.Group(pp.Optional(NOT) + variable + operator + value)
        simple_expr.set_parse_action(handle_simple_rule)

        # Use infixNotation to handle operator precedence (AND before OR),
        # associativity (left-to-right), and parentheses.
        expr_parser = pp.infixNotation(
            simple_expr,
            [
                (AND, 2, pp.opAssoc.LEFT, handle_binary_op),
                (OR, 2, pp.opAssoc.LEFT, handle_binary_op),
            ],
        ) 

        return expr_parser

    def parse(self, expression_string: str) -> str:
        """
        Parses a given expression string into a JSON configuration string.

        Args:
            expression_string: The expression to parse.

        Returns:
            A compact JSON string representing the configuration, or an error message.
        """
        if not expression_string.strip():
            return "{}"
            
        try:
            # parseString returns a ParseResults object; the actual result is the first element.
            result = self._parser.parseString(expression_string, parseAll=True)[0]
            # # Convert the final dictionary to a compact JSON string.
            # return json.dumps(result, separators=(',', ':'))
            return result
        except pp.ParseException as e:
            return f"Error: Syntax error in expression at char {e.loc}. {e.msg}"
        except Exception as e:
            return f"An unexpected error occurred: {e}"
        


# --- Subcommand Functions ---

def subcommand_show(args, config: Config):
    # This was a placeholder, let's make it useful or remove it
    if args.element == "proxies":
        subcommand_proxy_list(args, config)
    elif args.element == "chains":
        subcommand_chain_list(args, config)
    elif args.element == "tables":
        subcommand_route_list_tables(args, config) # List tables first
        tables = config.get_routing_tables()
        if tables:
             print("\nUse 'route list <table>' to see rules for a specific table.")
        else:
             print("No routing tables defined.")
    elif args.element == "routes":
        for table in config.get_routing_tables():
            args.table = table  # Temporarily set table for route listing
            subcommand_route_list(args, config)
            print("")
        args.table = None  # Reset table
    elif args.element == "servers":
        subcommand_server_list(args, config)
    elif args.element == "hosts":
        subcommand_hosts_list(args, config)
    elif args.element == "all":
        subcommand_server_list(args, config)
        print("")

        subcommand_proxy_list(args, config)
        print("")

        subcommand_chain_list(args, config)
        print("")

        for table in config.get_routing_tables():
            args.table = table  # Temporarily set table for route listing
            subcommand_route_list(args, config)
            print("")
        args.table = None  # Reset table

        subcommand_hosts_list(args, config)
        print("")
    else:
        print(f"Unknown element '{args.element}'")


# --- Host Subcommands ---
def subcommand_hosts_list(args, config):
    print("--- Hosts ---")
    hosts = config.get_hosts()
    if not hosts:
        print("No hosts defined.")
        return
    max_len = max(len(name) for name in hosts.keys()) if hosts else 0
    for name, ip in hosts.items():
        print(f"{name:<{max_len}} -> {ip}")

def subcommand_hosts_add(args, config):
    print(f"Adding host '{args.name}' with IP '{args.ip}'...")
    config.add_host(args.name, args.ip)

def subcommand_hosts_del(args, config):
    if args.name == "all":
         print("Deleting all hosts...")
         config.delete_host()
    else:
        print(f"Deleting host '{args.name}'...")
        config.delete_host(args.name)

def subcommand_hosts_update(args, config: Config):
    print(f"Updating host '{args.name}' to IP '{args.ip}'" + (f" and renaming to '{args.newName}'" if args.newName else "") + "...")
    config.update_host(args.name, ip=args.ip, newName=args.newName)

# --- Proxy Subcommands ---
def subcommand_proxy_add(args, config):
    name = args.name if args.name else config._get_unused_proxy_name()
    connstring = f"{args.protocol}://{args.host}:{args.port}"
    print(f"Adding proxy '{name}' ({connstring})...")
    config.add_proxy(name, connstring, user=args.user, password=args.password)

def subcommand_proxy_list(args, config: Config):
    print("--- Proxies ---")
    proxies = config.get_proxies()
    if not proxies:
        print("No proxies defined.")
        return
    for name, data in proxies.items():
        conn = data.get(Config.PROXY_KEY_CONNSTRING, "N/A")
        user = f", User: {data[Config.PROXY_KEY_USER]}" if Config.PROXY_KEY_USER in data else ""
        pw = f", Pass: {data[Config.PROXY_KEY_PASS]}" if Config.PROXY_KEY_PASS in data else ""
        print(f"{name}: {conn}{user}{pw}")

def subcommand_proxy_del(args, config):
    if args.proxy == "all":
        print("Deleting all proxies...")
        config.delete_proxy()
    else:
        print(f"Deleting proxy '{args.proxy}'...")
        config.delete_proxy(args.proxy)

def subcommand_proxy_update(args, config):
    print(f"Updating proxy '{args.proxy}'...")
    config.update_proxy(args.proxy, protocol=args.protocol, host=args.host, port=args.port, user=args.user, password=args.password, newName=args.name)

# --- Chain Subcommands ---
def subcommand_chain_list(args, config):
    print("--- Chains ---")
    chains = config.get_chains()
    if not chains:
        print("No chains defined.")
        return
    for name, data in chains.items():
        proxies = " -> ".join(data.get(Config.CHAIN_KEY_PROXIES, []))
        read_timeout = f"|RT: {data[Config.CHAIN_KEY_TCPREADTIMEOUT]}" if Config.CHAIN_KEY_TCPREADTIMEOUT in data else ""
        conn_timeout = f"|CT: {data[Config.CHAIN_KEY_TCPCONNECTTIMEOUT]}" if Config.CHAIN_KEY_TCPCONNECTTIMEOUT in data else ""
        proxy_dns = f"ProxyDNS" if Config.CHAIN_KEY_PROXYDNS in data else ""
        opts = f"{proxy_dns}{read_timeout}{conn_timeout}"
        if opts !="":
            opts = f" [{opts}]"
        print(f"{name}: {proxies}{opts}")

def subcommand_chain_add(args, config):
    name = args.name if args.name else config._get_unused_chain_name()
    print(f"Adding chain '{name}' with proxies {args.proxies}...")
    config.add_chain(name, args.proxies, tcpReadTimeout=args.tcpReadTimeout, tcpConnectTimeout=args.tcpConnectTimeout, proxyDns=args.proxyDns)

def subcommand_chain_del(args, config):
    if args.name == "all":
        print("Deleting all chains...")
        config.delete_chain()
    else:
        print(f"Deleting chain '{args.name}'...")
        config.delete_chain(args.name)

def subcommand_chain_update(args, config):
    print(f"Updating chain '{args.name}'...")
    config.update_chain(args.name, proxies=args.proxies, tcpReadTimeout=args.tcpReadTimeout, tcpConnectTimeout=args.tcpConnectTimeout, proxyDns=args.proxyDns, newName=args.newName) # Pass newName correctly

# --- Server Subcommands ---
def subcommand_server_list(args, config):
    print("--- Servers ---")
    servers = config.get_servers()
    if not servers:
        print("No servers defined.")
        return
    for index, server in enumerate(servers):
        print(f"{index}: {server}")

def subcommand_server_add(args, config):
    print(f"Adding server {args.protocol}://{args.host}:{args.port} using table '{args.table}'...")
    config.add_server(args.protocol, args.host, args.port, args.table)

def subcommand_server_add_fwd(args, config: Config):
    connstring = f"fwd://{args.local_host}:{args.local_port}:{args.chain}:{args.remote_host}:{args.remote_port}"
    print(f"Adding forwarder {connstring}...")
    config.add_server_fwd(args.local_host, args.local_port, args.chain, args.remote_host, args.remote_port)
    # Note: The `add_server` method may need to be updated to handle the full forwarder format.

def subcommand_server_del(args, config):
    if args.index == "all":
        print("Deleting all servers...")
        config.delete_server()
    else:
        print(f"Deleting server at index {args.index}...")
        config.delete_server(args.index)

def subcommand_server_update(args, config):
    print(f"Updating server at index {args.index}...")
    config.update_server(args.index, protocol=args.protocol, host=args.host, port=args.port, table=args.table)

# --- Route Subcommands ---
def subcommand_route_list_tables(args, config: Config):
    print("--- Routing Tables ---")
    tables = config.get_routing_tables()
    if not tables:
        print("No routing tables defined.")
    else:
        for table_name in tables:
            table = config.get_routes(table_name)
            if table:
                default_route = table.get(config.TABLE_KEY_DEFAULT, "N/A")
                count = len(table.get(config.TABLE_KEY_BLOCKS, []))
                print(f"- {table_name} (Default: {default_route}, {count} rule{'s' if count != 1 else ''})")

def subcommand_route_update_default(args, config: Config):
    print(f"Updating default route for table '{args.table}' to '{args.default}'...")
    config.update_default_route(args.table, args.default)

def subcommand_route_list(args, config: Config):
    table_name = args.table
    print(f"--- Rules for Table: {table_name} ---")
    table = config.get_routes(table_name)
    if table is None:
         print(f"Table '{table_name}' does not exist.")
         return

    default_route = table.get(config.TABLE_KEY_DEFAULT, "N/A")
    print(f"Default route: {default_route}")
    blocks = table.get(config.TABLE_KEY_BLOCKS, [])
    if not blocks:
        print(f"No rules defined in table '{table_name}'.")
        return
    
    for index, route_block in enumerate(blocks):
        rules_json = json.dumps(route_block.get(Config.ROUTEBLOCK_KEY_RULES, {}))  # Compact JSON
        target = route_block.get(Config.ROUTEBLOCK_KEY_ROUTE, "N/A")
        comment = f" # {route_block.get(Config.ROUTEBLOCK_KEY_COMMENT, '')}" if Config.ROUTEBLOCK_KEY_COMMENT in route_block else ""
        disabled = " [DISABLED]" if route_block.get(Config.ROUTEBLOCK_KEY_DISABLE, False) else ""
        print(f"{index}:{disabled} IF {rules_json} THEN route via '{target}'{comment}")


def subcommand_route_add(args, config: Config):
    print(f"Parsing rule expression: '{args.expression}'")
    parser = RuleParser(config)
    rule_dict = parser.parse(args.expression)

    if rule_dict:
        print(f"Parsed rule: {json.dumps(rule_dict)}")
        print(f"Adding rule to table '{args.table}' routing via '{args.route_target}'...")
        config.add_route(
            args.table,
            rule_dict,
            args.route_target,
            comment=args.comment,
            position=args.position,
            disable=args.disable
        )
    else:
        print("Failed to add rule due to parsing error.")

def subcommand_route_update(args, config: Config):
    print(f"Updating rule at index {args.index} in table '{args.table}'...")
    parser = RuleParser(config)

    # Parse the rule expression if provided
    rule_dict = None
    if args.expression:
        print(f"Parsing rule expression: '{args.expression}'")
        rule_dict = parser.parse(args.expression)
        if isinstance(rule_dict, str):  # If parsing failed, it will return an error string
            print(rule_dict)
            return

    # Determine disable state
    disable = None
    if args.disable:
        disable = True
    elif args.enable:
        disable = False

    # Update the route
    config.update_route(
        args.table,
        args.index,
        rule_dict=rule_dict,
        route_target=args.route_target,
        comment=args.comment,
        disable=disable,
        new_index=args.new_index
    )

def subcommand_route_del(args, config):
    print(f"Deleting rule at index {args.index} from table '{args.table}'...")
    config.delete_route(args.table, args.index)

def subcommand_route_del_table(args, config):
     print(f"Deleting routing table '{args.table}'...")
     config.delete_routing_table(args.table)


# --- Main Execution ---
def main():
    # Determine default config path
    config_dir_base = os.environ.get('XDG_CONFIG_HOME', os.path.expanduser('~/.config'))
    default_config_dir = os.path.join(config_dir_base, 'bbs')
    default_config_path = os.path.join(default_config_dir, 'bbs.json')

    parser_global = argparse.ArgumentParser(prog="bbscli",description="bbs configuration helper tool.",epilog="Manage your bbs proxy chains, routes, and servers easily.")
    parser_global.add_argument("-c", "--config",help=f"Path to the bbs JSON configuration file (default: {default_config_path})",default=default_config_path)
    subparsers_global = parser_global.add_subparsers(title="Available Commands",help="Action to perform on the configuration",required=True,dest="command") # Changed from subcommand to command for clarity


    # --- Show Command ---
    parser_show = subparsers_global.add_parser("show", help="Show sections of the current config")
    parser_show.add_argument("element", choices=["proxies", "chains", "tables", "routes", "servers", "hosts", "all"], default="all", nargs="?", help="Config section to show (default: all)")
    parser_show.set_defaults(func=subcommand_show) # Will handle 'all' internally

    # --- Host Command ---
    parser_host = subparsers_global.add_parser("host", help="Manage static host entries (/etc/hosts style)")
    subparsers_host = parser_host.add_subparsers(title="Host Actions", dest="host_action", required=True)

    parser_hosts_list = subparsers_host.add_parser("list", help="List all host entries")
    parser_hosts_list.set_defaults(func=subcommand_hosts_list)

    parser_host_add = subparsers_host.add_parser("add", help="Add a new host entry")
    parser_host_add.add_argument("name", help="Hostname")
    parser_host_add.add_argument("ip", help="IP address")
    parser_host_add.set_defaults(func=subcommand_hosts_add)

    parser_host_del = subparsers_host.add_parser("del", help="Delete a host entry")
    parser_host_del.add_argument("name", help='Hostname to delete, or "all"')
    parser_host_del.set_defaults(func=subcommand_hosts_del)

    parser_host_update = subparsers_host.add_parser("update", help="Update an existing host entry")
    parser_host_update.add_argument("name", help="Current hostname to update")
    parser_host_update.add_argument("-i", "--ip", help="New IP address")
    parser_host_update.add_argument("-n", "--newName", help="Optional new hostname")
    parser_host_update.set_defaults(func=subcommand_hosts_update)

    # --- Proxy Command ---
    parser_proxy = subparsers_global.add_parser("proxy", help="Manage proxy definitions")
    subparsers_proxy = parser_proxy.add_subparsers(title="Proxy Actions", dest="proxy_action", required=True)

    parser_proxy_list = subparsers_proxy.add_parser("list", help="List all proxies")
    parser_proxy_list.set_defaults(func=subcommand_proxy_list)

    parser_proxy_add = subparsers_proxy.add_parser("add" , help="Add a new proxy")
    parser_proxy_add.add_argument("protocol", choices=["socks5", "http", "https"], help="Proxy protocol (use http for HTTP/HTTPS proxies)") # Added https for clarity, maps to http type internally? Assume bbs handles it.
    parser_proxy_add.add_argument("host", help="Proxy server hostname or IP")
    parser_proxy_add.add_argument("port", help="Proxy server port")
    parser_proxy_add.add_argument("-u", "--user", help="Username for proxy authentication")
    parser_proxy_add.add_argument("-p", "--password", help="Password for proxy authentication")
    parser_proxy_add.add_argument("-n", "--name", help="Optional name for the proxy (auto-generated if omitted)")
    parser_proxy_add.set_defaults(func=subcommand_proxy_add)

    parser_proxy_del = subparsers_proxy.add_parser("del", help="Delete a proxy")
    parser_proxy_del.add_argument("proxy", help='Name of the proxy to delete, or "all"')
    parser_proxy_del.set_defaults(func=subcommand_proxy_del)

    parser_proxy_update = subparsers_proxy.add_parser("update", help="Update an existing proxy")
    parser_proxy_update.add_argument("proxy", help="Name of the proxy to update")
    parser_proxy_update.add_argument("-t", "--protocol", choices=["socks5", "http", "https"], help="New proxy protocol")
    parser_proxy_update.add_argument("-H", "--host", help="New proxy server hostname or IP")
    parser_proxy_update.add_argument("-P", "--port", help="New proxy server port")
    parser_proxy_update.add_argument("-u", "--user", help="New username (provide empty string '' to remove)")
    parser_proxy_update.add_argument("-p", "--password", help="New password (provide empty string '' to remove)")
    parser_proxy_update.add_argument("-n", "--name", help="Optional new name for the proxy")
    parser_proxy_update.set_defaults(func=subcommand_proxy_update)

    # --- Chain Command ---
    parser_chain = subparsers_global.add_parser("chain", help="Manage proxy chains")
    subparsers_chain = parser_chain.add_subparsers(title="Chain Actions", dest="chain_action", required=True)

    parser_chain_list = subparsers_chain.add_parser("list", help="List all proxy chains")
    parser_chain_list.set_defaults(func=subcommand_chain_list)

    parser_chain_add = subparsers_chain.add_parser("add", help="Add a new proxy chain")
    parser_chain_add.add_argument("proxies", nargs="+", help="List of proxy names in the order they should be chained")
    parser_chain_add.add_argument("-rt", "--tcpReadTimeout", type=int, help="TCP read timeout in milliseconds")
    parser_chain_add.add_argument("-ct", "--tcpConnectTimeout", type=int, help="TCP connect timeout in milliseconds")
    parser_chain_add.add_argument("-pd", "--proxyDns", action=argparse.BooleanOptionalAction, help="Enable/disable DNS resolution via proxy")
    parser_chain_add.add_argument("-n", "--name", help="Optional name for the chain (auto-generated if omitted)")
    parser_chain_add.set_defaults(func=subcommand_chain_add)

    parser_chain_del = subparsers_chain.add_parser("del", help="Delete a proxy chain")
    parser_chain_del.add_argument("name", help='Name of the chain to delete, or "all"')
    parser_chain_del.set_defaults(func=subcommand_chain_del)

    parser_chain_update = subparsers_chain.add_parser("update", help="Update an existing proxy chain")
    parser_chain_update.add_argument("name", help="Name of the chain to update")
    parser_chain_update.add_argument("-p", "--proxies", nargs="+", help="New list of proxy names for the chain")
    parser_chain_update.add_argument("-rt", "--tcpReadTimeout", type=int, help="New TCP read timeout in ms")
    parser_chain_update.add_argument("-ct", "--tcpConnectTimeout", type=int, help="New TCP connect timeout in ms")
    parser_chain_update.add_argument("-pd", "--proxyDns", action=argparse.BooleanOptionalAction, help="Enable/disable DNS resolution via proxy")
    parser_chain_update.add_argument("-N", "--newName", help="Optional new name for the chain") # Use -N to avoid conflict with -n in add
    parser_chain_update.set_defaults(func=subcommand_chain_update)

    # --- Server Command ---
    parser_server = subparsers_global.add_parser("server", help="Manage listening servers")
    subparsers_server = parser_server.add_subparsers(title="Server Actions", dest="server_action", required=True)

    parser_server_list = subparsers_server.add_parser("list", help="List all listening servers")
    parser_server_list.set_defaults(func=subcommand_server_list)

    parser_server_add = subparsers_server.add_parser("add", help="Add a new listening server")
    parser_server_add.add_argument("protocol", choices=["socks5", "http"], help="Server protocol")
    parser_server_add.add_argument("host", help="Host/IP to listen on (e.g., 127.0.0.1, 0.0.0.0)")
    parser_server_add.add_argument("port", help="Port to listen on")
    parser_server_add.add_argument("table", help="Routing table name to use for this server")
    parser_server_add.set_defaults(func=subcommand_server_add)

    # Add Forwarder
    parser_server_add_fwd = subparsers_server.add_parser("add-fwd", help="Add a new forwarder")
    parser_server_add_fwd.add_argument("local_host", help="Local host/IP to listen on")
    parser_server_add_fwd.add_argument("local_port", help="Local port to listen on")
    parser_server_add_fwd.add_argument("chain", help="Proxy chain name to use")
    parser_server_add_fwd.add_argument("remote_host", help="Remote host/IP to forward to")
    parser_server_add_fwd.add_argument("remote_port", help="Remote port to forward to")
    parser_server_add_fwd.set_defaults(func=subcommand_server_add_fwd)

    parser_server_del = subparsers_server.add_parser("del", help="Delete a listening server")
    parser_server_del.add_argument("index", help='Index of the server to delete (from "server list"), or "all"')
    parser_server_del.set_defaults(func=subcommand_server_del)

    parser_server_update = subparsers_server.add_parser("update", help="Update an existing listening server")
    parser_server_update.add_argument("index", help="Index of the server to update")
    parser_server_update.add_argument("-t", "--protocol", choices=["socks5", "http", "fwd"], help="New server protocol")
    parser_server_update.add_argument("-H", "--host", help="New host/IP to listen on")
    parser_server_update.add_argument("-P", "--port", help="New port to listen on")
    parser_server_update.add_argument("-T", "--table", help="New routing table name") # Use -T for table
    parser_server_update.set_defaults(func=subcommand_server_update)


    # --- Route Command ---
    parser_route = subparsers_global.add_parser("route", help="Manage routing rules and tables")
    subparsers_route = parser_route.add_subparsers(title="Route Actions", dest="route_action", required=True)

    # List Tables
    parser_route_list_tables = subparsers_route.add_parser("list-tables", help="List all routing table names")
    parser_route_list_tables.set_defaults(func=subcommand_route_list_tables)

    # Update default route
    parser_route_update_default = subparsers_route.add_parser("update-default", help="Update the default route for a table")
    parser_route_update_default.add_argument("table", help="Name of the routing table")
    parser_route_update_default.add_argument("default", help="New default route (e.g., 'direct', 'drop', or a chain name)")
    parser_route_update_default.set_defaults(func=subcommand_route_update_default)

    # List Rules in a Table
    parser_route_list = subparsers_route.add_parser("list", help="List rules within a specific routing table")
    parser_route_list.add_argument("table", help="Name of the routing table")
    parser_route_list.set_defaults(func=subcommand_route_list)

    # Add Rule
    parser_route_add = subparsers_route.add_parser("add", help="Add a new routing rule to a table")
    parser_route_add.add_argument("table", help="Name of the table to add the rule to (will be created if it doesn't exist)")
    parser_route_add.add_argument("expression", help="Rule expression (e.g., 'host is 1.1.1.1', 'port is 80 or port is 443', 'not host in 192.168.1.0/24')")
    parser_route_add.add_argument("route_target", help="Target chain name or 'drop'")
    parser_route_add.add_argument("-p", "--position", type=int, help="Position index to insert the rule at (default: append)")
    parser_route_add.add_argument("--comment", help="Optional comment for the rule")
    parser_route_add.add_argument("--disable", action="store_true", help="Add the rule but keep it disabled")
    parser_route_add.set_defaults(func=subcommand_route_add)

    # Update Rule
    parser_route_update = subparsers_route.add_parser("update", help="Update an existing routing rule in a table")
    parser_route_update.add_argument("table", help="Name of the table containing the rule")
    parser_route_update.add_argument("index", help="Index of the rule to update (from 'route list <table>')")
    parser_route_update.add_argument("-e", "--expression", help="New rule expression (e.g., 'host is 1.1.1.1')")
    parser_route_update.add_argument("-t", "--route_target", help="New target chain name or 'drop'")
    parser_route_update.add_argument("--comment", help="New comment for the rule")
    parser_route_update.add_argument("--disable", action="store_true", help="Disable the rule")
    parser_route_update.add_argument("--enable", action="store_true", help="Enable the rule (overrides --disable)")
    parser_route_update.add_argument("--new-index", type=int, help="New index to move the rule to")
    parser_route_update.set_defaults(func=subcommand_route_update) 

    # Delete Rule
    parser_route_del = subparsers_route.add_parser("del", help="Delete a rule from a table by its index")
    parser_route_del.add_argument("table", help="Name of the table containing the rule")
    parser_route_del.add_argument("index", help='Index of the rule to delete (from "route list <table>")')
    parser_route_del.set_defaults(func=subcommand_route_del)

    # Delete Table
    parser_route_del_table = subparsers_route.add_parser("del-table", help="Delete an entire routing table")
    parser_route_del_table.add_argument("table", help="Name of the routing table to delete")
    parser_route_del_table.set_defaults(func=subcommand_route_del_table)

    # --- Parse Arguments ---
    args = parser_global.parse_args()

    # --- Initialize Config ---
    config = Config(args.config)

    # --- Execute Subcommand Function ---
    if hasattr(args, 'func') and callable(args.func):
        args.func(args, config)
    else:
        # This shouldn't happen if subparsers are required, but good practice
        parser_global.print_help()


if __name__ == '__main__':
    main()

