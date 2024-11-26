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
	CheckBy                 string           `json:"check_by"`
	CheckAt                 time.Time        `json:"check_at"`
	PingOk                  bool             `json:"ping_ok"`
	PingStable              bool             `json:"ping_stable"`
	IP                      string           `json:"ip"`
	RunID                   string           `json:"runid"`
	IsMaster                bool             `json:"is_master"`
	IsOffline               bool             `json:"is_offline"`
	IsReadOnly              bool             `json:"is_read_only"`
	IsReplPaused            bool             `json:"is_repl_paused"`
	MasterReplicationOffset int64            `json:"master_replication_offset"`
	SecondReplicationOffset int64            `json:"second_replication_offset"`
	ReplicationBacklogStart int64            `json:"replication_backlog_start"`
	ReplicationBacklogSize  int64            `json:"replication_backlog_size"`
	ReplicationID           string           `json:"replication_id"`
	ReplicationID2          string           `json:"replication_id2"`
	Error                   string           `json:"error"`
	ConnectedReplicas       []string         `json:"connected_replicas"`
	ReplicaState            *ReplicaState    `json:"replica_state"`
	SentiCacheState         *SentiCacheState `json:"senticache_state"`
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
	MasterLinkState      bool   `json:"master_link_state"`
	MasterLinkDownTime   int64  `json:"master_link_down_time"`
	MasterSyncInProgress bool   `json:"master_sync_in_progress"`
	ReplicationOffset    int64  `json:"replication_offset"`
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
	From        string              `json:"from"`
	To          string              `json:"to"`
	Cause       string              `json:"cause"`
	InitiatedBy string              `json:"initiated_by"`
	InitiatedAt time.Time           `json:"initiated_at"`
	StartedBy   string              `json:"started_by"`
	StartedAt   time.Time           `json:"started_at"`
	Result      *SwitchoverResult   `json:"result"`
	Progress    *SwitchoverProgress `json:"progress"`
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
	Ok         bool      `json:"ok"`
	Error      string    `json:"error"`
	FinishedAt time.Time `json:"finished_at"`
}

// SwitchoverProgress contains intents and status of running switchover
type SwitchoverProgress struct {
	Version    int    `json:"version"`
	Phase      int    `json:"phase"`
	NewMaster  string `json:"new_master"`
	MostRecent string `json:"most_recent"`
}

// Maintenance struct presence means that cluster under manual control
type Maintenance struct {
	InitiatedBy  string    `json:"initiated_by"`
	InitiatedAt  time.Time `json:"initiated_at"`
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
	Applied     bool      `json:"applied"`
	InitiatedAt time.Time `json:"initiated_at"`
	InitiatedBy string    `json:"initiated_by"`
	TargetHost  string    `json:"target_host"`
	Cause       string    `json:"cause"`
}

func (pp *PoisonPill) String() string {
	ms := "entering"
	if pp.Applied {
		ms = "on"
	}
	return fmt.Sprintf("<%s by %s for %s at %s: %s>", ms, pp.InitiatedBy, pp.TargetHost, pp.InitiatedAt, pp.Cause)
}
