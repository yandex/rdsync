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

// ValkeyConfig contains valkey connection info and params
type ValkeyConfig struct {
	AuthPassword            string        `yaml:"auth_password"`
	AofPath                 string        `yaml:"aof_path"`
	RestartCommand          string        `yaml:"restart_command"`
	TLSCAPath               string        `yaml:"tls_ca_path"`
	AuthUser                string        `yaml:"auth_user"`
	FailoverCooldown        time.Duration `yaml:"failover_cooldown"`
	WaitPromoteTimeout      time.Duration `yaml:"wait_promote_timeout"`
	WriteTimeout            time.Duration `yaml:"write_timeout"`
	DNSTTL                  time.Duration `yaml:"dns_ttl"`
	FailoverTimeout         time.Duration `yaml:"failover_timeout"`
	Port                    int           `yaml:"port"`
	RestartTimeout          time.Duration `yaml:"restart_timeout"`
	WaitReplicationTimeout  time.Duration `yaml:"wait_replication_timeout"`
	WaitCatchupTimeout      time.Duration `yaml:"wait_catchup_timeout"`
	DialTimeout             time.Duration `yaml:"dial_timeout"`
	WaitPromoteForceTimeout time.Duration `yaml:"wait_promote_force_timeout"`
	WaitPoisonPillTimeout   time.Duration `yaml:"wait_poison_pill_timeout"`
	MaxParallelSyncs        int           `yaml:"max_parallel_syncs"`
	ClusterBusPort          int           `yaml:"cluster_bus_port"`
	TurnBeforeSwitchover    bool          `yaml:"turn_before_switchover"`
	UseTLS                  bool          `yaml:"use_tls"`
	AllowDataLoss           bool          `yaml:"allow_data_loss"`
}

// SentinelModeConfig contains sentinel-mode specific configuration
type SentinelModeConfig struct {
	Name                string `yaml:"name"`
	RunID               string `yaml:"run_id"`
	ClusterName         string `yaml:"cluster_name"`
	CacheAuthUser       string `yaml:"cache_auth_user"`
	CacheAuthPassword   string `yaml:"cache_auth_password"`
	CacheRestartCommand string `yaml:"cache_restart_command"`
	CacheUpdateSecret   string `yaml:"cache_update_secret"`
	TLSCAPath           string `yaml:"tls_ca_path"`
	CachePort           int    `yaml:"cache_port"`
	AnnounceHostname    bool   `yaml:"announce_hostname"`
	UseTLS              bool   `yaml:"use_tls"`
}

// Config contains rdsync application configuration
type Config struct {
	Mode                    string              `yaml:"mode"`
	InfoFile                string              `yaml:"info_file"`
	Hostname                string              `yaml:"hostname"`
	LogLevel                string              `yaml:"loglevel"`
	AofMode                 string              `yaml:"aof_mode"`
	MaintenanceFile         string              `yaml:"maintenance_file"`
	DaemonLockFile          string              `yaml:"daemon_lock_file"`
	PprofAddr               string              `yaml:"pprof_addr"`
	SentinelMode            SentinelModeConfig  `yaml:"sentinel_mode"`
	Zookeeper               dcs.ZookeeperConfig `yaml:"zookeeper"`
	Valkey                  ValkeyConfig        `yaml:"valkey"`
	HealthCheckInterval     time.Duration       `yaml:"healthcheck_interval"`
	InfoFileHandlerInterval time.Duration       `yaml:"info_file_handler_interval"`
	InactivationDelay       time.Duration       `yaml:"inactivation_delay"`
	DcsWaitTimeout          time.Duration       `yaml:"dcs_wait_timeout"`
	TickInterval            time.Duration       `yaml:"tick_interval"`
	PingStable              int                 `yaml:"ping_stable"`
}

// DefaultValkeyConfig returns default configuration for valkey connection info and params
func DefaultValkeyConfig() ValkeyConfig {
	return ValkeyConfig{
		Port:                    6379,
		ClusterBusPort:          16379,
		UseTLS:                  false,
		TLSCAPath:               "",
		AuthUser:                "",
		AuthPassword:            "",
		DialTimeout:             5 * time.Second,
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
		RestartCommand:          "systemctl restart valkey-server",
		AofPath:                 "",
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
		CachePort:           26379,
		CacheRestartCommand: "systemctl restart valkey-senticache",
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
		Valkey:                  DefaultValkeyConfig(),
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
