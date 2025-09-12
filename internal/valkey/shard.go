package valkey

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/yandex/rdsync/internal/config"
	"github.com/yandex/rdsync/internal/dcs"
)

// Shard contains a set of valkey nodes
type Shard struct {
	dcs    dcs.DCS
	config *config.Config
	logger *slog.Logger
	nodes  map[string]*Node
	local  *Node
	sync.Mutex
}

// NodeConfiguration is a dcs node configuration for valkey replica
type NodeConfiguration struct {
	// Priority - is a host priority to become master. Can be changed via CLI.
	Priority int `json:"priority"`
}

// NewShard is a Shard constructor
func NewShard(config *config.Config, logger *slog.Logger, dcs dcs.DCS) *Shard {
	s := &Shard{
		config: config,
		logger: logger.With(slog.String("module", "shard")),
		nodes:  make(map[string]*Node),
		local:  nil,
		dcs:    dcs,
	}
	return s
}

// GetShardHostsFromDcs returns current shard hosts from dcs state
func (s *Shard) GetShardHostsFromDcs() ([]string, error) {
	fqdns, err := s.dcs.GetChildren(dcs.PathHANodesPrefix)
	if err == dcs.ErrNotFound {
		return make([]string, 0), nil
	}
	if err != nil {
		return nil, err
	}

	return fqdns, nil
}

// UpdateHostsInfo reads host names from DCS and updates shard state
func (s *Shard) UpdateHostsInfo() error {
	s.Lock()
	defer s.Unlock()

	hosts, err := s.GetShardHostsFromDcs()
	if err != nil {
		return err
	}
	s.logger.Info(fmt.Sprintf("Nodes from DCS: %s", hosts))
	set := make(map[string]int, len(hosts))
	for _, host := range hosts {
		set[host]++
	}

	for host := range set {
		if _, found := s.nodes[host]; !found {
			var node *Node
			if node, err = NewNode(s.config, s.logger, host); err != nil {
				return err
			}
			s.nodes[host] = node
			if s.local == nil && node.IsLocal() {
				s.local = node
			}
		}
	}
	// we delete hosts which are no longer in dcs
	for hostname := range s.nodes {
		if _, found := set[hostname]; !found {
			if s.local == nil || hostname != s.local.FQDN() {
				s.nodes[hostname].Close()
			}
			delete(s.nodes, hostname)
		}
	}

	return nil
}

// Get returns Valkey Node by host name
func (s *Shard) Get(host string) *Node {
	s.Lock()
	defer s.Unlock()

	return s.nodes[host]
}

// Local returns Valkey Node running on the same not as current rdsync process
func (s *Shard) Local() *Node {
	return s.local
}

// Close closes all established connections to nodes
func (s *Shard) Close() {
	s.Lock()
	defer s.Unlock()

	for _, node := range s.nodes {
		node.Close()
	}
}

// Hosts returns all nodes from local state
func (s *Shard) Hosts() []string {
	s.Lock()
	defer s.Unlock()

	var hosts []string
	for host := range s.nodes {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	return hosts
}

// GetNodeConfiguration returns current node configuration from dcs
func (s *Shard) GetNodeConfiguration(host string) (*NodeConfiguration, error) {
	var nc NodeConfiguration
	err := s.dcs.Get(dcs.JoinPath(dcs.PathHANodesPrefix, host), &nc)
	if err != nil {
		if err != dcs.ErrNotFound && err != dcs.ErrMalformed {
			return nil, fmt.Errorf("failed to get Priority for host %s: %s", host, err)
		}
		return DefaultNodeConfiguration(), nil
	}

	return &nc, nil
}

// DefaultNodeConfiguration returns default node configuration (matches upstream sentinel settings)
func DefaultNodeConfiguration() *NodeConfiguration {
	return &NodeConfiguration{Priority: 100}
}
