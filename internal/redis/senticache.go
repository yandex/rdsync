package redis

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	client "github.com/redis/go-redis/v9"

	"github.com/yandex/rdsync/internal/config"
)

// SentiCacheSentinel represents the "other" senticache in senticache
type SentiCacheSentinel struct {
	Name  string
	RunID string
	IP    string
	Port  int
}

// SentiCacheReplica represents the redis replica as seen by senticache
type SentiCacheReplica struct {
	IP                    string
	Port                  int
	RunID                 string
	MasterLinkDownTime    int64
	SlavePriority         int
	ReplicaAnnounced      int
	MasterHost            string
	MasterPort            int
	SlaveMasterLinkStatus int
	SlaveReplOffset       int64
}

// SentiCacheMaster represents the redis master as seen by senticache
type SentiCacheMaster struct {
	Name          string
	IP            string
	Port          int
	RunID         string
	Quorum        int
	ParallelSyncs int
	ConfigEpoch   uint64
}

// SentiCacheState represents the desired senticache state
type SentiCacheState struct {
	Master    SentiCacheMaster
	Replicas  []SentiCacheReplica
	Sentinels []SentiCacheSentinel
}

// SentiCacheNode represents API to query/manipulate a single Redis SentiCache node
type SentiCacheNode struct {
	config *config.Config
	logger *slog.Logger
	conn   *client.Client
	broken bool
}

// NewSentiCacheNode is a SentiCacheNode constructor
func NewSentiCacheNode(config *config.Config, logger *slog.Logger) (*SentiCacheNode, error) {
	addr := net.JoinHostPort(localhost, strconv.Itoa(config.SentinelMode.CachePort))
	opts := client.Options{
		Addr:            addr,
		Username:        config.SentinelMode.CacheAuthUser,
		Password:        config.SentinelMode.CacheAuthPassword,
		DialTimeout:     100 * time.Millisecond,
		ReadTimeout:     100 * time.Millisecond,
		PoolSize:        1,
		MinIdleConns:    1,
		ConnMaxLifetime: time.Hour,
		Protocol:        2,
	}
	if config.SentinelMode.UseTLS {
		tlsConf, err := getTLSConfig(config, config.SentinelMode.TLSCAPath, localhost)
		if err != nil {
			return nil, err
		}
		opts.TLSConfig = tlsConf
	}
	node := SentiCacheNode{
		config: config,
		conn:   client.NewClient(&opts),
		logger: logger.With("module", "senticache"),
		broken: false,
	}
	return &node, nil
}

// Close closes underlying Redis connection
func (s *SentiCacheNode) Close() error {
	return s.conn.Close()
}

func (s *SentiCacheNode) restart(ctx context.Context) error {
	s.logger.Error("Restarting broken senticache")
	splitted := strings.Fields(s.config.SentinelMode.CacheRestartCommand)
	cmd := exec.CommandContext(ctx, splitted[0], splitted[1:]...)
	return cmd.Run()
}

func (s *SentiCacheNode) sentinels(ctx context.Context) ([]SentiCacheSentinel, error) {
	cmd := client.NewSliceCmd(ctx, "SENTINEL", "SENTINELS", "1")
	err := s.conn.Process(ctx, cmd)
	if err != nil {
		return []SentiCacheSentinel{}, err
	}
	val, err := cmd.Result()
	if err != nil {
		return []SentiCacheSentinel{}, err
	}
	res := make([]SentiCacheSentinel, len(val))
	for index, rawSentinel := range val {
		sentinel := SentiCacheSentinel{}
		sentinelInt := rawSentinel.([]interface{})
		for i := 0; i < len(sentinelInt)/2; i += 2 {
			key := sentinelInt[i].(string)
			value := sentinelInt[i+1].(string)
			switch key {
			case "name":
				sentinel.Name = value
			case "ip":
				sentinel.IP = value
			case "port":
				sentinel.Port, err = strconv.Atoi(value)
				if err != nil {
					return res, fmt.Errorf("port in senticache sentinel %d: %s", index, err.Error())
				}
			case "runid":
				sentinel.RunID = value
			}
		}
		res[index] = sentinel
	}
	return res, nil
}

func (s *SentiCacheNode) master(ctx context.Context) (*SentiCacheMaster, error) {
	cmd := client.NewSliceCmd(ctx, "SENTINEL", "MASTERS")
	err := s.conn.Process(ctx, cmd)
	if err != nil {
		return nil, err
	}
	val, err := cmd.Result()
	if err != nil {
		return nil, err
	}
	if len(val) == 0 {
		return nil, nil
	} else if len(val) > 1 {
		return nil, fmt.Errorf("got %d masters in senticache", len(val))
	}
	var res SentiCacheMaster
	master := val[0].([]interface{})
	for i := 0; i < len(master)/2; i += 2 {
		key := master[i].(string)
		value := master[i+1].(string)
		switch key {
		case "name":
			res.Name = value
		case "ip":
			res.IP = value
		case "port":
			res.Port, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("port in senticache master reply: %s", err.Error())
			}
		case "runid":
			res.RunID = value
		case "quorum":
			res.Quorum, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("quorum in senticache master reply: %s", err.Error())
			}
		case "parallel-syncs":
			res.ParallelSyncs, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parallel-syncs in senticache master reply: %s", err.Error())
			}
		case "config-epoch":
			res.ConfigEpoch, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("config-epoch in senticache master reply: %s", err.Error())
			}
		default:
			continue
		}
	}
	return &res, nil
}

func (s *SentiCacheNode) replicas(ctx context.Context) ([]SentiCacheReplica, error) {
	cmd := client.NewSliceCmd(ctx, "SENTINEL", "REPLICAS", "1")
	err := s.conn.Process(ctx, cmd)
	if err != nil {
		return []SentiCacheReplica{}, err
	}
	val, err := cmd.Result()
	if err != nil {
		return []SentiCacheReplica{}, err
	}
	res := make([]SentiCacheReplica, len(val))
	for index, rawReplica := range val {
		replica := SentiCacheReplica{}
		replicaInt := rawReplica.([]interface{})
		for i := 0; i < len(replicaInt)/2; i += 2 {
			key := replicaInt[i].(string)
			value := replicaInt[i+1].(string)
			switch key {
			case "ip":
				replica.IP = value
			case "port":
				replica.Port, err = strconv.Atoi(value)
				if err != nil {
					return res, fmt.Errorf("port in senticache replica %d: %s", index, err.Error())
				}
			case "runid":
				replica.RunID = value
			case "master-link-down-time":
				replica.MasterLinkDownTime, err = strconv.ParseInt(value, 10, 64)
				if err != nil {
					return res, fmt.Errorf("master-link-down-time in senticache replica %d: %s", index, err.Error())
				}
			case "slave-priority":
				replica.SlavePriority, err = strconv.Atoi(value)
				if err != nil {
					return res, fmt.Errorf("slave-priority in senticache replica %d: %s", index, err.Error())
				}
			case "replica-announced":
				replica.ReplicaAnnounced, err = strconv.Atoi(value)
				if err != nil {
					return res, fmt.Errorf("replica-announced in senticache replica %d: %s", index, err.Error())
				}
			case "master-host":
				replica.MasterHost = value
			case "master-port":
				replica.MasterPort, err = strconv.Atoi(value)
				if err != nil {
					return res, fmt.Errorf("master-port in senticache replica %d: %s", index, err.Error())
				}
			case "master-link-status":
				if value == "ok" {
					replica.SlaveMasterLinkStatus = 0
				} else {
					replica.SlaveMasterLinkStatus = 1
				}
			case "slave-repl-offset":
				replica.SlaveReplOffset, err = strconv.ParseInt(value, 10, 64)
				if err != nil {
					return res, fmt.Errorf("slave-repl-offset in senticache replica %d: %s", index, err.Error())
				}
			}
		}
		res[index] = replica
	}
	return res, nil
}

// Update sets in-memory state of senticache
func (s *SentiCacheNode) Update(ctx context.Context, state *SentiCacheState) error {
	if s.broken {
		err := s.restart(ctx)
		if err != nil {
			return err
		}
		s.broken = false
	}
	var sentinels []SentiCacheSentinel
	var replicas []SentiCacheReplica
	master, err := s.master(ctx) // Just validate that structure is correct
	if err != nil {
		s.broken = true
		return err
	}
	if master != nil {
		sentinels, err = s.sentinels(ctx)
		if err != nil {
			s.broken = true
			return err
		}
		replicas, err = s.replicas(ctx)
		if err != nil {
			s.broken = true
			return err
		}
	}
	s.logger.Debug(fmt.Sprintf("Previous state: master: %v, replicas: %v, sentinels: %v", master, replicas, sentinels))
	var command = []string{
		"SENTINEL", "CACHE-UPDATE", s.config.SentinelMode.CacheUpdateSecret,
		"master-name:", state.Master.Name + ",",
		"master-addr:", state.Master.IP, fmt.Sprintf("%d,", state.Master.Port),
		"master-spec:", state.Master.RunID, fmt.Sprintf("%d", state.Master.Quorum),
		fmt.Sprintf("%d", state.Master.ParallelSyncs),
		fmt.Sprintf("%d,", state.Master.ConfigEpoch)}
	dropSentinels := make([]SentiCacheSentinel, 0)
	for _, sentinel := range sentinels {
		found := false
		for _, targetSentinel := range state.Sentinels {
			if sentinel.Name == targetSentinel.Name {
				found = true
				break
			}
		}
		if !found {
			dropSentinels = append(dropSentinels, sentinel)
		}
	}
	for _, sentinel := range dropSentinels {
		command = append(command, "delete-sentinel:", sentinel.Name+",")
	}
	dropReplicas := make([]SentiCacheReplica, 0)
	for _, replica := range replicas {
		found := false
		for _, targetReplica := range state.Replicas {
			if replica.IP == targetReplica.IP && replica.Port == targetReplica.Port {
				found = true
				break
			}
		}
		if !found {
			dropReplicas = append(dropReplicas, replica)
		}
	}
	for _, replica := range dropReplicas {
		command = append(command, "delete-replica:", replica.IP, fmt.Sprintf("%d,", replica.Port))
	}
	for _, sentinel := range state.Sentinels {
		command = append(command, "add-sentinel:", sentinel.Name, sentinel.RunID, sentinel.IP, fmt.Sprintf("%d,", sentinel.Port))
	}
	for _, replica := range state.Replicas {
		command = append(command, "add-replica:", replica.IP, fmt.Sprintf("%d,", replica.Port),
			"slave-spec:", replica.IP, fmt.Sprintf("%d", replica.Port), replica.RunID, fmt.Sprintf("%d", replica.MasterLinkDownTime),
			fmt.Sprintf("%d", replica.SlavePriority), fmt.Sprintf("%d", replica.ReplicaAnnounced), replica.MasterHost,
			fmt.Sprintf("%d", replica.MasterPort), fmt.Sprintf("%d", replica.SlaveMasterLinkStatus), fmt.Sprintf("%d,", replica.SlaveReplOffset),
		)
	}
	s.logger.Debug(fmt.Sprintf("Updating senticache state with %v", command))
	cmd := make([]interface{}, len(command))
	for i, v := range command {
		cmd[i] = v
	}
	res := s.conn.Do(ctx, cmd...)
	err = res.Err()
	if err != nil {
		s.broken = true
		return err
	}
	return nil
}
