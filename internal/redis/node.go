package redis

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	client "github.com/redis/go-redis/v9"

	"github.com/yandex/rdsync/internal/config"
)

const (
	localhost       = "127.0.0.1"
	highMinReplicas = 65535
)

// Node represents API to query/manipulate a single Redis node
type Node struct {
	config      *config.Config
	logger      *slog.Logger
	fqdn        string
	ips         []net.IP
	ipsTime     time.Time
	clusterID   string
	infoResults []bool
	cachedInfo  map[string]string
	conn        *client.Client
}

func uniqLookup(host string) ([]net.IP, error) {
	ret := make([]net.IP, 0)
	res, err := net.LookupIP(host)
	if err != nil {
		return ret, err
	}
	seen := map[string]struct{}{}
	for _, ip := range res {
		key := string(ip)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			ret = append(ret, ip)
		}
	}
	return ret, err
}

// NewNode is a Node constructor
func NewNode(config *config.Config, logger *slog.Logger, fqdn string) (*Node, error) {
	var host string
	if fqdn == config.Hostname {
		// Offline mode forbids connections on non-lo interfaces
		host = localhost
	} else {
		host = fqdn
	}
	nodeLogger := logger.With("module", "node", "fqdn", host)
	now := time.Now()
	ips, err := uniqLookup(fqdn)
	if err != nil {
		nodeLogger.Warn("Dns lookup failed", "error", err)
		ips = []net.IP{}
		now = time.Time{}
	}
	addr := net.JoinHostPort(host, strconv.Itoa(config.Redis.Port))
	opts := client.Options{
		Addr:            addr,
		Username:        config.Redis.AuthUser,
		Password:        config.Redis.AuthPassword,
		DialTimeout:     config.Redis.DialTimeout,
		ReadTimeout:     config.Redis.ReadTimeout,
		WriteTimeout:    config.Redis.WriteTimeout,
		PoolSize:        1,
		MaxRetries:      -1,
		ConnMaxIdleTime: -1,
		Protocol:        2,
	}
	if config.Redis.UseTLS {
		tlsConf, err := getTLSConfig(config, config.Redis.TLSCAPath, host)
		if err != nil {
			return nil, err
		}
		opts.TLSConfig = tlsConf
	}
	node := Node{
		clusterID: "",
		config:    config,
		conn:      client.NewClient(&opts),
		logger:    nodeLogger,
		fqdn:      fqdn,
		ips:       ips,
		ipsTime:   now,
	}
	return &node, nil
}

// FQDN returns Node fqdn
func (n *Node) FQDN() string {
	return n.fqdn
}

// IsLocal returns true if Node running on the same host as calling rdsync process
func (n *Node) IsLocal() bool {
	return n.fqdn == n.config.Hostname
}

func (n *Node) String() string {
	return n.fqdn
}

// Close closes underlying Redis connection
func (n *Node) Close() error {
	return n.conn.Close()
}

// MatchHost checks if node has target hostname or ip
func (n *Node) MatchHost(host string) bool {
	if n.fqdn == host {
		return true
	}
	hostIP := net.ParseIP(host)
	if hostIP == nil {
		return false
	}
	for _, ip := range n.ips {
		if hostIP.Equal(ip) {
			return true
		}
	}
	return false
}

// RefreshAddrs updates internal ip address list if ttl exceeded
func (n *Node) RefreshAddrs() error {
	if time.Since(n.ipsTime) < n.config.Redis.DNSTTL {
		n.logger.Debug("Not updating ips cache due to ttl")
		return nil
	}
	n.logger.Debug("Updating ips cache")
	now := time.Now()
	ips, err := uniqLookup(n.fqdn)
	if err != nil {
		n.logger.Error("Updating ips cache failed", "error", err)
		return err
	}
	n.ips = ips
	n.ipsTime = now
	return nil
}

// GetIP returns first ip as string
func (n *Node) GetIP() (string, error) {
	for _, ip := range n.ips {
		return ip.String(), nil
	}
	return "", fmt.Errorf("unable to find a usable ip for %s", n.fqdn)
}

// GetIPs returns a string slice of node ips
func (n *Node) GetIPs() []string {
	var ret []string
	for _, ip := range n.ips {
		ret = append(ret, ip.String())
	}
	return ret
}

func (n *Node) configRewrite(ctx context.Context) error {
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "rewrite")
	return setCmd.Err()
}

// IsReplPaused returns pause status of replication on node
func (n *Node) IsReplPaused(ctx context.Context) (bool, error) {
	cmd := client.NewStringSliceCmd(ctx, n.config.Renames.Config, "get", "repl-paused")
	err := n.conn.Process(ctx, cmd)
	if err != nil {
		return false, err
	}
	vals, err := cmd.Result()
	if err != nil {
		return false, err
	}
	if len(vals) != 2 {
		return false, fmt.Errorf("unexpected config get result for repl-paused: %v", vals)
	}
	return vals[1] == "yes", nil
}

// PauseReplication pauses replication from master on node
func (n *Node) PauseReplication(ctx context.Context) error {
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "set", "repl-paused", "yes")
	err := setCmd.Err()
	if err != nil {
		return err
	}
	return n.configRewrite(ctx)
}

// ResumeReplication starts replication from master on node
func (n *Node) ResumeReplication(ctx context.Context) error {
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "set", "repl-paused", "no")
	err := setCmd.Err()
	if err != nil {
		return err
	}
	return n.configRewrite(ctx)
}

// IsOffline returns Offline status for node
func (n *Node) IsOffline(ctx context.Context) (bool, error) {
	cmd := client.NewStringSliceCmd(ctx, n.config.Renames.Config, "get", "offline")
	err := n.conn.Process(ctx, cmd)
	if err != nil {
		return false, err
	}
	vals, err := cmd.Result()
	if err != nil {
		return false, err
	}
	if len(vals) != 2 {
		return false, fmt.Errorf("unexpected config get result for offline: %v", vals)
	}
	return vals[1] == "yes", nil
}

// SetOffline disallows non-localhost connections and drops all existing clients (except rdsync ones)
func (n *Node) SetOffline(ctx context.Context) error {
	if !n.IsLocal() {
		return fmt.Errorf("making %s offline is not possible - not local", n.fqdn)
	}
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "set", "offline", "yes")
	err := setCmd.Err()
	if err != nil {
		return err
	}
	err = n.DisconnectClients(ctx, "normal")
	if err != nil {
		return err
	}
	err = n.DisconnectClients(ctx, "pubsub")
	if err != nil {
		return err
	}
	return nil
}

// DisconnectClients disconnects all connected clients with specified type
func (n *Node) DisconnectClients(ctx context.Context, ctype string) error {
	disconnectCmd := n.conn.Do(ctx, n.config.Renames.Client, "kill", "type", ctype)
	return disconnectCmd.Err()
}

// GetMinReplicas returns number of connected replicas to accept writes on node
func (n *Node) GetMinReplicas(ctx context.Context) (int, error) {
	cmd := client.NewStringSliceCmd(ctx, n.config.Renames.Config, "get", "min-replicas-to-write")
	err := n.conn.Process(ctx, cmd)
	if err != nil {
		return 0, err
	}
	vals, err := cmd.Result()
	if err != nil {
		return 0, err
	}
	if len(vals) != 2 {
		return 0, fmt.Errorf("unexpected config get result for min-replicas-to-write: %v", vals)
	}
	ret, err := strconv.ParseInt(vals[1], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("unable to parse min-replicas-to-write value: %s", err.Error())
	}
	return int(ret), nil
}

// SetMinReplicas sets desired number of connected replicas to accept writes on node
func (n *Node) SetMinReplicas(ctx context.Context, value int) (error, error) {
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "set", "min-replicas-to-write", strconv.Itoa(value))
	err := setCmd.Err()
	if err != nil {
		return err, nil
	}
	return err, n.configRewrite(ctx)
}

// GetQuorumReplicas returns a set of quorum replicas
func (n *Node) GetQuorumReplicas(ctx context.Context) (string, error) {
	cmd := client.NewStringSliceCmd(ctx, n.config.Renames.Config, "get", "quorum-replicas")
	err := n.conn.Process(ctx, cmd)
	if err != nil {
		return "", err
	}
	vals, err := cmd.Result()
	if err != nil {
		return "", err
	}
	if len(vals) != 2 {
		return "", fmt.Errorf("unexpected config get result for quorum-replicas: %v", vals)
	}
	splitted := strings.Split(vals[1], " ")
	sort.Strings(splitted)
	return strings.Join(splitted, " "), nil
}

// SetQuorumReplicas sets desired quorum replicas
func (n *Node) SetQuorumReplicas(ctx context.Context, value string) (error, error) {
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "set", "quorum-replicas", value)
	err := setCmd.Err()
	if err != nil {
		return err, nil
	}
	return err, n.configRewrite(ctx)
}

// EmptyQuorumReplicas sets quorum replicas to empty value (as it should be on replicas)
func (n *Node) EmptyQuorumReplicas(ctx context.Context) error {
	quorumReplicas, err := n.GetQuorumReplicas(ctx)
	if err != nil {
		return err
	}
	if quorumReplicas != "" {
		err, rewriteErr := n.SetQuorumReplicas(ctx, "")
		if err != nil {
			return err
		}
		if rewriteErr != nil {
			n.logger.Error("Rewrite config failed", "error", rewriteErr)
		}
	}
	return nil
}

// GetAppendonly returns a setting of appendonly config
func (n *Node) GetAppendonly(ctx context.Context) (bool, error) {
	cmd := client.NewStringSliceCmd(ctx, n.config.Renames.Config, "get", "appendonly")
	err := n.conn.Process(ctx, cmd)
	if err != nil {
		return false, err
	}
	vals, err := cmd.Result()
	if err != nil {
		return false, err
	}
	if len(vals) != 2 {
		return false, fmt.Errorf("unexpected config get result for repl-paused: %v", vals)
	}
	return vals[1] == "yes", nil
}

// SetOffline disallows non-localhost connections and drops all existing clients (except rdsync ones)
func (n *Node) SetAppendonly(ctx context.Context, value bool) error {
	strValue := "yes"
	if !value {
		strValue = "no"
	}
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "set", "appendonly", strValue)
	err := setCmd.Err()
	if err != nil {
		return err
	}
	return n.configRewrite(ctx)
}

// IsReadOnly returns ReadOnly status for node
func (n *Node) IsReadOnly(ctx context.Context) (bool, error) {
	minReplicas, err := n.GetMinReplicas(ctx)
	if err != nil {
		return false, err
	}
	return minReplicas == highMinReplicas, nil
}

// SetReadOnly makes node read-only by setting min replicas to unreasonably high value and disconnecting clients
func (n *Node) SetReadOnly(ctx context.Context, disconnect bool) (error, error) {
	err, rewriteErr := n.SetMinReplicas(ctx, highMinReplicas)
	if err != nil {
		return err, rewriteErr
	}
	if disconnect {
		err = n.DisconnectClients(ctx, "normal")
		if err != nil {
			return err, rewriteErr
		}
		err = n.DisconnectClients(ctx, "pubsub")
		if err != nil {
			return err, rewriteErr
		}
	}
	return nil, rewriteErr
}

// SetOnline allows non-localhost connections
func (n *Node) SetOnline(ctx context.Context) error {
	if !n.IsLocal() {
		return fmt.Errorf("making %s online is not possible - not local", n.fqdn)
	}
	setCmd := n.conn.Do(ctx, n.config.Renames.Config, "set", "offline", "no")
	return setCmd.Err()
}

// Restart restarts redis server
func (n *Node) Restart(ctx context.Context) error {
	if !n.IsLocal() {
		return fmt.Errorf("restarting %s is not possible - not local", n.fqdn)
	}
	n.logger.Warn(fmt.Sprintf("Restarting with %s", n.config.Redis.RestartCommand))
	splitted := strings.Fields(n.config.Redis.RestartCommand)
	cmd := exec.CommandContext(ctx, splitted[0], splitted[1:]...)
	return cmd.Run()
}

// GetInfo returns raw info map
func (n *Node) GetInfo(ctx context.Context) (map[string]string, error) {
	cmd := n.conn.Info(ctx)
	err := cmd.Err()
	if err != nil {
		n.infoResults = append(n.infoResults, false)
		if len(n.infoResults) > n.config.PingStable {
			n.infoResults = n.infoResults[1:]
		}
		clearCache := true
		for _, result := range n.infoResults {
			if result {
				clearCache = false
				break
			}
		}
		if clearCache {
			n.cachedInfo = nil
		}
		return n.cachedInfo, err
	}

	inp := cmd.Val()
	lines := strings.Count(inp, "\r\n")
	res := make(map[string]string, lines)
	pos := 0
	for {
		endIndex := strings.Index(inp[pos:], "\r\n")
		if endIndex == -1 {
			break
		}
		pair := inp[pos : pos+endIndex]
		pos += endIndex + 2
		sepIndex := strings.Index(pair, ":")
		if sepIndex == -1 {
			continue
		}
		res[pair[:sepIndex]] = pair[sepIndex+1:]
	}
	n.infoResults = append(n.infoResults, true)
	if len(n.infoResults) > n.config.PingStable {
		n.infoResults = n.infoResults[1:]
	}
	n.cachedInfo = res
	return res, nil
}

func (n *Node) EvaluatePing() (bool, bool) {
	res := false
	stable := true
	for _, result := range n.infoResults {
		if result {
			res = true
		} else {
			stable = false
		}
	}
	return res, stable
}

// SentinelMakeReplica makes node replica of target in sentinel mode
func (n *Node) SentinelMakeReplica(ctx context.Context, target string) error {
	if n.fqdn == target {
		return fmt.Errorf("making %s replica of itself is not possible", n.fqdn)
	}
	err := n.EmptyQuorumReplicas(ctx)
	if err != nil {
		return err
	}
	cmd := n.conn.Do(ctx, n.config.Renames.ReplicaOf, target, n.config.Redis.Port)
	err = cmd.Err()
	if err != nil {
		return err
	}
	return n.configRewrite(ctx)
}

// SentinelPromote makes node primary in sentinel mode
func (n *Node) SentinelPromote(ctx context.Context) error {
	cmd := n.conn.Do(ctx, n.config.Renames.ReplicaOf, "NO", "ONE")
	err := cmd.Err()
	if err != nil {
		return err
	}
	return n.configRewrite(ctx)
}

// ClusterGetID returns cluster node id of node
func (n *Node) ClusterGetID(ctx context.Context) (string, error) {
	if n.clusterID != "" {
		return n.clusterID, nil
	}
	cmd := client.NewStringCmd(ctx, n.config.Renames.Cluster, n.config.Renames.ClusterMyID)
	err := n.conn.Process(ctx, cmd)
	if err != nil {
		return "", err
	}
	clusterID, err := cmd.Result()
	if err != nil {
		return "", err
	}
	n.clusterID = clusterID
	return n.clusterID, nil
}

// ClusterMakeReplica makes node replica of target in cluster mode
func (n *Node) ClusterMakeReplica(ctx context.Context, targetID string) error {
	err := n.EmptyQuorumReplicas(ctx)
	if err != nil {
		return err
	}
	cmd := n.conn.Do(ctx, n.config.Renames.Cluster, n.config.Renames.ClusterReplicate, targetID)
	return cmd.Err()
}

// IsClusterMajorityAlive checks if majority of masters in cluster are not failed
func (n *Node) IsClusterMajorityAlive(ctx context.Context) (bool, error) {
	cmd := n.conn.ClusterNodes(ctx)
	err := cmd.Err()
	if err != nil {
		return false, err
	}
	totalMasters := 0
	failedMasters := 0
	lines := strings.Split(cmd.Val(), "\n")
	for _, line := range lines {
		splitted := strings.Split(line, " ")
		if len(splitted) < 3 {
			continue
		}
		if strings.Contains(splitted[2], "master") {
			totalMasters += 1
			if strings.Contains(splitted[2], "fail") {
				failedMasters += 1
			}
		}
	}
	res := failedMasters < totalMasters/2+1
	n.logger.Debug(fmt.Sprintf("Cluster majority alive check: %d total, %d failed -> %t", totalMasters, failedMasters, res))
	return res, nil
}

// ClusterPromoteForce makes node primary in cluster mode if master/majority of masters is reachable
func (n *Node) ClusterPromoteForce(ctx context.Context) error {
	cmd := n.conn.Do(ctx, n.config.Renames.Cluster, n.config.Renames.ClusterFailover, "FORCE")
	return cmd.Err()
}

// ClusterPromoteTakeover makes node primary in cluster mode if majority of masters is not reachable
func (n *Node) ClusterPromoteTakeover(ctx context.Context) error {
	cmd := n.conn.Do(ctx, n.config.Renames.Cluster, n.config.Renames.ClusterFailover, "TAKEOVER")
	return cmd.Err()
}

// IsClusterNodeAlone checks if node sees only itself
func (n *Node) IsClusterNodeAlone(ctx context.Context) (bool, error) {
	cmd := n.conn.ClusterNodes(ctx)
	err := cmd.Err()
	if err != nil {
		return false, err
	}
	lines := strings.Split(cmd.Val(), "\n")
	var count int
	for _, line := range lines {
		if len(strings.TrimSpace(line)) > 0 {
			count++
		}
	}
	return count == 1, nil
}

// ClusterMeet makes replica join the cluster
func (n *Node) ClusterMeet(ctx context.Context, addr string, port, clusterBusPort int) error {
	cmd := n.conn.Do(ctx, n.config.Renames.Cluster, n.config.Renames.ClusterMeet, addr, strconv.Itoa(port), strconv.Itoa(clusterBusPort))
	return cmd.Err()
}

// HasClusterSlots checks if node has any slot assigned
func (n *Node) HasClusterSlots(ctx context.Context) (bool, error) {
	cmd := n.conn.ClusterNodes(ctx)
	err := cmd.Err()
	if err != nil {
		return false, err
	}
	lines := strings.Split(cmd.Val(), "\n")
	for _, line := range lines {
		splitted := strings.Split(line, " ")
		if len(splitted) < 3 {
			continue
		}
		if strings.Contains(splitted[2], "myself") {
			return len(splitted) > 8, nil
		}
	}
	return false, nil
}
