package main

// Defines the structures, interfaces and functions needed to parse JSON formatted routing rules and to evaluate addresses against these rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"regexp"
	"sync"
)

// routingConf is the type used to hold and access a routing configuration (defined in a file)
type routingConf struct {
	routing routing
	valid   bool // whether the current configuration is valid
	mu      sync.RWMutex
}

type routing map[string]routingTable

// Holds the ordered list of rule blocks that constitutes the core of the routing model. See README.md#Configuration##routing JSON configuration
type routingTable []ruleBlock

// Maps the JSON fields described in README.md#Configuration##Routing JSON configuration
type ruleBlock struct {
	Comment string
	Rules   evaluater
	Route   string
}

// Maps the JSON fields described in README.md#Configuration##Routing JSON configuration
type ruleCombo struct {
	Rule1 evaluater
	Op    string
	Rule2 evaluater
}

// Maps the JSON fields described in README.md#Configuration##Routing JSON configuration
type rule struct {
	Rule     string
	Variable string
	Content  string
	Negate   bool
}

// An interface describing routing rule-ish objects that, given a destination address, return a decision (true or false).
// Rule and RuleCombo types implement the evaluater interface.
type evaluater interface {
	// evaluate reports whether the destination address string addr matches the criteria defined by the Evaluater
	evaluate(addr string) (bool, error)
}

func (r rule) evaluate(addr string) (bool, error) {

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		err = fmt.Errorf("error spliting host and port : %v", err)
		return true, err
	}

	switch r.Rule {
	case "regexp":
		var variable string
		switch r.Variable {
		case "host":
			variable = host
		case "port":
			variable = port
		case "addr":
			variable = addr
		default:
			err = fmt.Errorf("unknown variable : %v", r.Variable)
			return true, err
		}

		matched, err := regexp.Match(r.Content, []byte(variable))
		if err != nil {
			err = fmt.Errorf("error matching regexp :  %v", err)
			return true, err
		}
		return (r.Negate != matched), nil

	case "subnet":
		hostIPv4 := net.ParseIP(host).To4()
		if hostIPv4 == nil {
			//host is not an IPv4 representation
			return false, nil
		}
		_, network, err := net.ParseCIDR(r.Content)
		if err != nil {
			err = fmt.Errorf("error parsing CIDR : %v", err)
			return true, err
		}

		inSubnet := network.Contains(hostIPv4)
		return (r.Negate != inSubnet), nil

	case "true":
		return true, nil

	default:
		err = fmt.Errorf("unknown rule type : %v", r.Rule)
		return true, err
	}

}

func (r ruleCombo) evaluate(addr string) (bool, error) {

	r1, err := r.Rule1.evaluate(addr)
	if err != nil {
		err = fmt.Errorf("error evaluating rule 1 %v : %v", r.Rule1, err)
		return true, err
	}
	r2, err := r.Rule2.evaluate(addr)
	if err != nil {
		err = fmt.Errorf("error evaluating rule 2 %v : %v", r.Rule2, err)
		return true, err
	}

	switch r.Op {
	case "AND", "and", "And", "&", "&&":
		return r1 && r2, nil
	case "OR", "or", "Or", "|", "||":
		return r1 || r2, nil
	default:
		err = fmt.Errorf("unknown op : %v", r.Op)
		return true, err
	}
}

// Custom JSON unmarshaller describing how to parse a RuleCombo type
func (rCombo *ruleCombo) UnmarshalJSON(b []byte) error {
	type tmpRuleCombo struct {
		Rule1 json.RawMessage
		Op    string
		Rule2 json.RawMessage
	}

	var tmp tmpRuleCombo

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	err := dec.Decode(&tmp)
	if err != nil {
		err = fmt.Errorf("error unmarshalling '%s' in TmpRuleCombo : %v", b, err)
		return err
	}

	rCombo.Op = tmp.Op

	//Try to unmarshal Rule1 rawmessage into a Rule, if it fails, try into a RuleCombo
	var rule1 rule

	dec = json.NewDecoder(bytes.NewReader(tmp.Rule1))
	dec.DisallowUnknownFields()
	err = dec.Decode(&rule1)
	if err == nil {
		//Rule1 is a Rule
		rCombo.Rule1 = rule1
	} else {
		//Rule1 is not a Rule, try to unmarshal it into a RuleCombo
		var rc ruleCombo

		dec = json.NewDecoder(bytes.NewReader(tmp.Rule1))
		dec.DisallowUnknownFields()
		err2 := dec.Decode(&rc)
		if err2 != nil {
			//Rule1 is not a RuleCombo nor a Rule, return an error
			err = fmt.Errorf("error unmarshalling into Rule (%v) and into RuleCombo (%v)", err, err2)
			return err
		}
		//Rule1 is a RuleCombo
		rCombo.Rule1 = rc
	}

	//Try to unmarshal Rule1 rawmessage into a Rule, if it fails, try into a RuleCombo
	var rule2 rule

	dec = json.NewDecoder(bytes.NewReader(tmp.Rule2))
	dec.DisallowUnknownFields()
	err = dec.Decode(&rule2)
	if err == nil {
		//Rule2 is a Rule
		rCombo.Rule2 = rule2
	} else {
		//Rule1 is not a Rule, try to unmarshal it into a RuleCombo
		var rc2 ruleCombo

		dec = json.NewDecoder(bytes.NewReader(tmp.Rule2))
		dec.DisallowUnknownFields()
		err2 := dec.Decode(&rc2)
		if err2 != nil {
			//Rule2 is not a RuleCombo nor a Rule, return an error
			err = fmt.Errorf("error unmarshalling into Rule (%v) and into RuleCombo (%v)", err, err2)
			return err
		}
		//Rule2 is a RuleCombo
		rCombo.Rule2 = rc2
	}

	return nil
}

// Custom JSON unmarshaller describing how to parse a RuleBlock type
func (rBlock *ruleBlock) UnmarshalJSON(b []byte) error {
	type tmpBlock struct {
		Comment string
		Rules   json.RawMessage
		Route   string
	}

	var tmp tmpBlock

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	err := dec.Decode(&tmp)
	if err != nil {
		err = fmt.Errorf("error unmarshalling '%s' in TmpBlock : %v", b, err)
		return err
	}

	rBlock.Comment = tmp.Comment
	rBlock.Route = tmp.Route

	//Try to unmarshal Rules rawmessage into a Rule, if it fails, try into a RuleCombo
	var rule rule

	dec = json.NewDecoder(bytes.NewReader(tmp.Rules))
	dec.DisallowUnknownFields()
	err = dec.Decode(&rule)
	if err == nil {
		//Rules is a Rule
		rBlock.Rules = rule
	} else {
		//Rules is not a Rule, try to unmarshal it into a RuleCombo
		var rc ruleCombo

		dec = json.NewDecoder(bytes.NewReader(tmp.Rules))
		dec.DisallowUnknownFields()
		err2 := dec.Decode(&rc)
		if err2 != nil {
			//Rules is not a RuleCombo nor a Rule, return an error
			err = fmt.Errorf("error unmarshalling into Rule (%v) and into RuleCombo (%v)", err, err2)
			return err
		}
		//Rules is a RuleCombo
		rBlock.Rules = rc
	}
	return nil
}

// getRoute returns in route the chain to use for a given destination address string addr.
// For each RuleBlock of the routing table, it evaluates addr against the rules and stops at the first evaluation returning true.
func (table routingTable) getRoute(addr string) (route string, err error) {
	for _, rBlock := range table {
		matched, err := rBlock.Rules.evaluate(addr)
		if err != nil {
			err = fmt.Errorf("error evaluating %v : %v", rBlock.Rules, err)
			return "", err
		}
		if matched {
			gMetaLogger.Debugf("ruleBlock %v matched for address %v, using route %v", rBlock.Comment, addr, rBlock.Route)
			return rBlock.Route, nil
		}
	}
	err = fmt.Errorf("all blocks evaluated to false for %v", addr)
	return "", err
}

// parseRoutingConfig parses the JSON routing configuration file at configPath and returns a Routing variable representing this configuration.
func parseRoutingConfig(configPath string) (routing, error) {

	fileBytes, err := os.ReadFile(configPath)
	if err != nil {
		err := fmt.Errorf("error reading file %v : %v", configPath, err)
		return nil, err
	}

	var routingConfig routing

	dec := json.NewDecoder(bytes.NewReader(fileBytes))
	dec.DisallowUnknownFields()

	err = dec.Decode(&routingConfig)
	if err != nil {
		err = fmt.Errorf("error unmarshalling routing config file : %v", err)
		return nil, err
	}

	return routingConfig, nil
}
