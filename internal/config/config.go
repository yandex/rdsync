package config

import (
	"context"
	"crypto/sha512"
	"fmt"
	"os"
	"time"

	"github.com/heetch/confita"
	"github.com/heetch/confita/backend/file"

	"github.com/yandex/rdsync/internal/dcs"
)

// RedisConfig contains redis connection info and params
type RedisConfig struct {
	Port                    int           `yaml:"port"`
	ClusterBusPort          int           `yaml:"cluster_bus_port"`
	UseTLS                  bool          `yaml:"use_tls"`
	TLSCAPath               string        `yaml:"tls_ca_path"`
	AuthUser                string        `yaml:"auth_user"`
	AuthPassword            string        `yaml:"auth_password"`
	DialTimeout             time.Duration `yaml:"dial_timeout"`
	ReadTimeout             time.Duration `yaml:"read_timeout"`
	WriteTimeout            time.Duration `yaml:"write_timeout"`
	DNSTTL                  time.Duration `yaml:"dns_ttl"`
	FailoverTimeout         time.Duration `yaml:"failover_timeout"`
	FailoverCooldown        time.Duration `yaml:"failover_cooldown"`
	RestartTimeout          time.Duration `yaml:"restart_timeout"`
	WaitReplicationTimeout  time.Duration `yaml:"wait_replication_timeout"`
	WaitCatchupTimeout      time.Duration `yaml:"wait_catchup_timeout"`
	WaitPromoteTimeout      time.Duration `yaml:"wait_promote_timeout"`
	WaitPromoteForceTimeout time.Duration `yaml:"wait_promote_force_timeout"`
	WaitPoisonPillTimeout   time.Duration `yaml:"wait_poison_pill_timeout"`
	MaxParallelSyncs        int           `yaml:"max_parallel_syncs"`
	AllowDataLoss           bool          `yaml:"allow_data_loss"`
	TurnBeforeSwitchover    bool          `yaml:"turn_before_switchover"`
	RestartCommand          string        `yaml:"restart_command"`
	AofPath                 string        `yaml:"aof_path"`
}

// RedisRenamesConfig contains redis command renames
type RedisRenamesConfig struct {
	Client           string `yaml:"client"`
	Cluster          string `yaml:"cluster"`
	ClusterFailover  string `yaml:"cluster_failover"`
	ClusterMyID      string `yaml:"cluster_myid"`
	ClusterReplicate string `yaml:"cluster_replicate"`
	ClusterMeet      string `yaml:"cluster_meet"`
	Config           string `yaml:"config"`
	ReplicaOf        string `yaml:"replicaof"`
}

// SentinelModeConfig contains sentinel-mode specific configuration
type SentinelModeConfig struct {
	AnnounceHostname    bool          `yaml:"announce_hostname"`
	Name                string        `yaml:"name"`
	RunID               string        `yaml:"run_id"`
	ClusterName         string        `yaml:"cluster_name"`
	CacheAuthUser       string        `yaml:"cache_auth_user"`
	CacheAuthPassword   string        `yaml:"cache_auth_password"`
	CacheDialTimeout    time.Duration `yaml:"cache_dial_timeout"`
	CacheReadTimeout    time.Duration `yaml:"cache_read_timeout"`
	CacheWriteTimeout   time.Duration `yaml:"cache_write_timeout"`
	CachePort           int           `yaml:"cache_port"`
	CacheRestartCommand string        `yaml:"cache_restart_command"`
	CacheUpdateSecret   string        `yaml:"cache_update_secret"`
	UseTLS              bool          `yaml:"use_tls"`
	TLSCAPath           string        `yaml:"tls_ca_path"`
}

// Config contains rdsync application configuration
type Config struct {
	LogLevel                string              `yaml:"loglevel"`
	Hostname                string              `yaml:"hostname"`
	Mode                    string              `yaml:"mode"`
	AofMode                 string              `yaml:"aof_mode"`
	InfoFile                string              `yaml:"info_file"`
	MaintenanceFile         string              `yaml:"maintenance_file"`
	DaemonLockFile          string              `yaml:"daemon_lock_file"`
	PingStable              int                 `yaml:"ping_stable"`
	TickInterval            time.Duration       `yaml:"tick_interval"`
	InactivationDelay       time.Duration       `yaml:"inactivation_delay"`
	HealthCheckInterval     time.Duration       `yaml:"healthcheck_interval"`
	InfoFileHandlerInterval time.Duration       `yaml:"info_file_handler_interval"`
	PprofAddr               string              `yaml:"pprof_addr"`
	Zookeeper               dcs.ZookeeperConfig `yaml:"zookeeper"`
	DcsWaitTimeout          time.Duration       `yaml:"dcs_wait_timeout"`
	Redis                   RedisConfig         `yaml:"redis"`
	Renames                 RedisRenamesConfig  `yaml:"renames"`
	SentinelMode            SentinelModeConfig  `yaml:"sentinel_mode"`
}

// DefaultRedisConfig returns default configuration for redis connection info and params
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		Port:                    6379,
		ClusterBusPort:          16379,
		UseTLS:                  false,
		TLSCAPath:               "",
		AuthUser:                "",
		AuthPassword:            "",
		DialTimeout:             5 * time.Second,
		ReadTimeout:             5 * time.Second,
		WriteTimeout:            5 * time.Second,
		DNSTTL:                  5 * time.Minute,
		FailoverTimeout:         30 * time.Second,
		FailoverCooldown:        30 * time.Minute,
		RestartTimeout:          5 * time.Minute,
		WaitReplicationTimeout:  15 * time.Minute,
		WaitCatchupTimeout:      10 * time.Minute,
		WaitPromoteTimeout:      5 * time.Minute,
		WaitPromoteForceTimeout: 10 * time.Second,
		WaitPoisonPillTimeout:   30 * time.Second,
		MaxParallelSyncs:        1,
		AllowDataLoss:           false,
		TurnBeforeSwitchover:    false,
		RestartCommand:          "systemctl restart redis-server",
		AofPath:                 "",
	}
}

// DefaultRedisRenamesConfig returns default redis command renames
func DefaultRedisRenamesConfig() RedisRenamesConfig {
	return RedisRenamesConfig{
		Client:           "CLIENT",
		Cluster:          "CLUSTER",
		ClusterFailover:  "FAILOVER",
		ClusterMyID:      "MYID",
		ClusterReplicate: "REPLICATE",
		ClusterMeet:      "MEET",
		Config:           "CONFIG",
		ReplicaOf:        "REPLICAOF",
	}
}

func makeFakeRunID(hostname string) (string, error) {
	hash := sha512.New384()
	_, err := hash.Write([]byte(hostname))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil))[:40], nil
}

// DefaultSentinelModeConfig returns default sentinel-mode specific configuration
func DefaultSentinelModeConfig(hostname string) (SentinelModeConfig, error) {
	fakeRunID, err := makeFakeRunID(hostname)
	if err != nil {
		return SentinelModeConfig{}, err
	}
	conf := SentinelModeConfig{
		AnnounceHostname:    false,
		Name:                hostname,
		RunID:               fakeRunID,
		ClusterName:         "test-cluster",
		CacheAuthUser:       "",
		CacheAuthPassword:   "",
		CacheDialTimeout:    5 * time.Second,
		CacheReadTimeout:    5 * time.Second,
		CacheWriteTimeout:   5 * time.Second,
		CachePort:           26379,
		CacheRestartCommand: "systemctl restart redis-senticache",
		CacheUpdateSecret:   "",
		UseTLS:              false,
		TLSCAPath:           "",
	}
	return conf, nil
}

// DefaultConfig returns default configuration for RdSync
func DefaultConfig() (Config, error) {
	zkConfig, err := dcs.DefaultZookeeperConfig()
	if err != nil {
		return Config{}, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return Config{}, err
	}
	sentinelConf, err := DefaultSentinelModeConfig(hostname)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		AofMode:                 "Unspecified",
		LogLevel:                "Info",
		Hostname:                hostname,
		Mode:                    "Sentinel",
		InfoFile:                "/var/run/rdsync/rdsync.info",
		DaemonLockFile:          "/var/run/rdsync/rdsync.lock",
		MaintenanceFile:         "/var/run/rdsync/rdsync.maintenance",
		PingStable:              3,
		TickInterval:            5 * time.Second,
		InactivationDelay:       30 * time.Second,
		HealthCheckInterval:     5 * time.Second,
		InfoFileHandlerInterval: 30 * time.Second,
		PprofAddr:               "",
		Zookeeper:               zkConfig,
		DcsWaitTimeout:          10 * time.Second,
		Redis:                   DefaultRedisConfig(),
		Renames:                 DefaultRedisRenamesConfig(),
		SentinelMode:            sentinelConf,
	}
	return config, nil
}

// ReadFromFile reads config from file (not set values are replaced by default ones)
func ReadFromFile(configFile string) (*Config, error) {
	conf, err := DefaultConfig()
	if err != nil {
		return nil, err
	}
	loader := confita.NewLoader(file.NewBackend(configFile))
	if err = loader.Load(context.Background(), &conf); err != nil {
		err = fmt.Errorf("failed to load config from %s: %s", configFile, err.Error())
		return nil, err
	}
	return &conf, nil
}
