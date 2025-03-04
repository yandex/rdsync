package app

import (
	"fmt"
	"time"
)

type appState int

const (
	stateInit appState = iota
	stateManager
	stateCandidate
	stateLost
	stateMaintenance
)

func (s appState) String() string {
	switch s {
	case stateInit:
		return "Init"
	case stateManager:
		return "Manager"
	case stateCandidate:
		return "Candidate"
	case stateLost:
		return "Lost"
	case stateMaintenance:
		return "Maintenance"
	}
	return "Unknown"
}

type appMode int

const (
	modeSentinel appMode = iota
	modeCluster
)

func (m appMode) String() string {
	switch m {
	case modeSentinel:
		return "Sentinel"
	case modeCluster:
		return "Cluster"
	}
	return "Unknown"
}

func parseMode(mode string) (appMode, error) {
	switch mode {
	case "Sentinel":
		return modeSentinel, nil
	case "Cluster":
		return modeCluster, nil
	}
	return modeSentinel, fmt.Errorf("unknown mode: %s", mode)
}

type aofMode int

const (
	modeUnspecified aofMode = iota
	modeOn
	modeOff
	modeOnReplicas
)

func (m aofMode) String() string {
	switch m {
	case modeUnspecified:
		return "Unspecified"
	case modeOn:
		return "On"
	case modeOff:
		return "Off"
	case modeOnReplicas:
		return "OnReplicas"
	}
	return "Unknown"
}

func parseAofMode(mode string) (aofMode, error) {
	switch mode {
	case "Unspecified":
		return modeUnspecified, nil
	case "On":
		return modeOn, nil
	case "Off":
		return modeOff, nil
	case "OnReplicas":
		return modeOnReplicas, nil
	}
	return modeUnspecified, fmt.Errorf("unknown aof mode: %s", mode)
}

const (
	// manager's lock
	pathManagerLock = "manager"

	pathMasterNode = "master"

	// activeNodes are master + alive running HA replicas
	// structure: list of hosts(strings)
	pathActiveNodes = "active_nodes"

	// structure: pathHealthPrefix/hostname -> NodeState
	pathHealthPrefix = "health"

	// structure: single Switchover
	pathCurrentSwitch = "current_switch"

	// structure: single Switchover
	pathLastSwitch = "last_switch"

	// structure: single Switchover
	pathLastRejectedSwitch = "last_rejected_switch"

	// structure: single Maintenance
	pathMaintenance = "maintenance"

	// List of HA nodes. May be modified by external tools (e.g. remove node from HA-cluster)
	// structure: pathHANodes/hostname -> NodeConfiguration
	pathHANodes = "ha_nodes"

	// fence flag
	// structure: single PoisonPill
	pathPoisonPill = "poison_pill"
)

// HostState contains status check performed by some rdsync process
type HostState struct {
	CheckAt                 time.Time        `json:"check_at"`
	ReplicaState            *ReplicaState    `json:"replica_state"`
	SentiCacheState         *SentiCacheState `json:"senticache_state"`
	ReplicationID           string           `json:"replication_id"`
	IP                      string           `json:"ip"`
	RunID                   string           `json:"runid"`
	Error                   string           `json:"error"`
	ReplicationID2          string           `json:"replication_id2"`
	CheckBy                 string           `json:"check_by"`
	ConnectedReplicas       []string         `json:"connected_replicas"`
	ReplicationBacklogStart int64            `json:"replication_backlog_start"`
	SecondReplicationOffset int64            `json:"second_replication_offset"`
	MasterReplicationOffset int64            `json:"master_replication_offset"`
	ReplicationBacklogSize  int64            `json:"replication_backlog_size"`
	MinReplicasToWrite      int64            `json:"min_replicas_to_write"`
	IsReplPaused            bool             `json:"is_repl_paused"`
	IsReadOnly              bool             `json:"is_read_only"`
	IsOffline               bool             `json:"is_offline"`
	IsMaster                bool             `json:"is_master"`
	PingStable              bool             `json:"ping_stable"`
	PingOk                  bool             `json:"ping_ok"`
}

func (hs *HostState) String() string {
	ping := "ok"
	if !hs.PingOk {
		ping = "err"
	}
	const unknown = "???"
	repl := unknown
	var offset int64
	if hs.IsMaster {
		repl = "master"
		offset = hs.MasterReplicationOffset
	} else if hs.ReplicaState != nil {
		if hs.ReplicaState.MasterLinkState {
			repl = "ok"
		} else {
			repl = "err"
		}
		offset = hs.ReplicaState.ReplicationOffset
	}
	return fmt.Sprintf("<ping=%s repl=%s offset=%d>", ping, repl, offset)
}

// ReplicaState contains replica specific info.
// Master always has this state empty
type ReplicaState struct {
	MasterHost           string `json:"master_host"`
	MasterLinkDownTime   int64  `json:"master_link_down_time"`
	ReplicationOffset    int64  `json:"replication_offset"`
	MasterLastIOSeconds  int64  `json:"master_last_io_seconds"`
	MasterLinkState      bool   `json:"master_link_state"`
	MasterSyncInProgress bool   `json:"master_sync_in_progress"`
}

func (rs *ReplicaState) String() string {
	return fmt.Sprintf("<%s %v: %d>", rs.MasterHost, rs.MasterLinkState, rs.ReplicationOffset)
}

// SentiCacheState contains senticache specific info.
// Cluster-mode nodes has this state empty
type SentiCacheState struct {
	Name  string `json:"name"`
	RunID string `json:"runid"`
}

func (ss *SentiCacheState) String() string {
	return fmt.Sprintf("<%s %s>", ss.Name, ss.RunID)
}

const (
	// CauseManual means switchover was issued via command line
	CauseManual = "manual"
	// CauseWorker means switchover was initiated via DCS
	CauseWorker = "worker"
	// CauseAuto  means failover was started automatically by failure detection process
	CauseAuto = "auto"
)

// Switchover contains info about currently running or scheduled switchover/failover process
type Switchover struct {
	InitiatedAt time.Time           `json:"initiated_at"`
	StartedAt   time.Time           `json:"started_at"`
	Result      *SwitchoverResult   `json:"result"`
	Progress    *SwitchoverProgress `json:"progress"`
	From        string              `json:"from"`
	To          string              `json:"to"`
	Cause       string              `json:"cause"`
	InitiatedBy string              `json:"initiated_by"`
	StartedBy   string              `json:"started_by"`
	RunCount    int                 `json:"run_count"`
}

func (sw *Switchover) String() string {
	var state string
	if sw.Result != nil {
		if sw.Result.Ok {
			state = "done"
		} else {
			state = "error"
		}
	} else if !sw.StartedAt.IsZero() {
		state = "running"
	} else {
		state = "scheduled"
	}
	swFrom := "*"
	if sw.From != "" {
		swFrom = sw.From
	}
	swTo := "*"
	if sw.To != "" {
		swTo = sw.To
	}
	return fmt.Sprintf("<%s %s=>%s %s by %s at %s>", state, swFrom, swTo, sw.Cause, sw.InitiatedBy, sw.InitiatedAt)
}

// SwitchoverResult contains results of finished/failed switchover
type SwitchoverResult struct {
	FinishedAt time.Time `json:"finished_at"`
	Error      string    `json:"error"`
	Ok         bool      `json:"ok"`
}

// SwitchoverProgress contains intents and status of running switchover
type SwitchoverProgress struct {
	NewMaster  string `json:"new_master"`
	MostRecent string `json:"most_recent"`
	Version    int    `json:"version"`
	Phase      int    `json:"phase"`
}

// Maintenance struct presence means that cluster under manual control
type Maintenance struct {
	InitiatedAt  time.Time `json:"initiated_at"`
	InitiatedBy  string    `json:"initiated_by"`
	RdSyncPaused bool      `json:"rdsync_paused"`
	ShouldLeave  bool      `json:"should_leave"`
}

func (m *Maintenance) String() string {
	ms := "entering"
	if m.RdSyncPaused {
		ms = "on"
	}
	if m.ShouldLeave {
		ms = "leaving"
	}
	return fmt.Sprintf("<%s by %s at %s>", ms, m.InitiatedBy, m.InitiatedAt)
}

type PoisonPill struct {
	InitiatedAt time.Time `json:"initiated_at"`
	InitiatedBy string    `json:"initiated_by"`
	TargetHost  string    `json:"target_host"`
	Cause       string    `json:"cause"`
	Applied     bool      `json:"applied"`
}

func (pp *PoisonPill) String() string {
	ms := "entering"
	if pp.Applied {
		ms = "on"
	}
	return fmt.Sprintf("<%s by %s for %s at %s: %s>", ms, pp.InitiatedBy, pp.TargetHost, pp.InitiatedAt, pp.Cause)
}
