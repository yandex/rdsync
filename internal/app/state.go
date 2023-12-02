package app

import (
	"strconv"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) getHostState(fqdn string) *HostState {
	node := app.shard.Get(fqdn)
	var state HostState
	state.CheckAt = time.Now()
	state.CheckBy = app.config.Hostname
	if app.mode == modeSentinel && fqdn == app.config.Hostname {
		state.SentiCacheState = &SentiCacheState{
			Name:  app.config.SentinelMode.Name,
			RunID: app.config.SentinelMode.RunID,
		}
	}
	info, err := node.GetInfo(app.ctx)
	if err != nil {
		state.Error = err.Error()
		if len(info) == 0 {
			state.PingOk = false
			state.PingStable = false
			return &state
		}
	}
	state.PingOk, state.PingStable = node.EvaluatePing()
	var ok bool
	state.RunID, ok = info["run_id"]
	if !ok {
		state.Error = "No run_id in info"
		return &state
	}
	state.ReplicationID, ok = info["master_replid"]
	if !ok {
		state.Error = "No master_replid in info"
		return &state
	}
	state.ReplicationID2, ok = info["master_replid2"]
	if !ok {
		state.Error = "No master_replid2 in info"
		return &state
	}
	masterOffset, ok := info["master_repl_offset"]
	if !ok {
		state.Error = "No master_repl_offset in info"
		return &state
	}
	state.MasterReplicationOffset, err = strconv.ParseInt(masterOffset, 10, 64)
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	secondOffset, ok := info["second_repl_offset"]
	if !ok {
		state.Error = "No second_repl_offset in info"
		return &state
	}
	state.SecondReplicationOffset, err = strconv.ParseInt(secondOffset, 10, 64)
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	replBacklogFirstByte, ok := info["repl_backlog_first_byte_offset"]
	if !ok {
		state.Error = "No repl_backlog_first_byte_offset in info"
		return &state
	}
	state.ReplicationBacklogStart, err = strconv.ParseInt(replBacklogFirstByte, 10, 64)
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	replBacklogHistlen, ok := info["repl_backlog_histlen"]
	if !ok {
		state.Error = "No repl_backlog_histlen in info"
		return &state
	}
	state.ReplicationBacklogSize, err = strconv.ParseInt(replBacklogHistlen, 10, 64)
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	role, ok := info["role"]
	if !ok {
		state.Error = "No role in info"
		return &state
	}
	if role == "master" {
		state.IsMaster = true
	} else {
		state.IsMaster = false
		rs := ReplicaState{}
		rs.MasterHost, ok = info["master_host"]
		if !ok {
			state.Error = "Replica but no master_host in info"
			return &state
		}
		linkState, ok := info["master_link_status"]
		if !ok {
			state.Error = "Replica but no master_link_status in info"
			return &state
		}
		rs.MasterLinkState = (linkState == "up")
		syncInProgress, ok := info["master_sync_in_progress"]
		if !ok {
			state.Error = "Replica but no master_sync_in_progress in info"
			return &state
		}
		rs.MasterSyncInProgress = (syncInProgress != "0")
		if !rs.MasterLinkState && !rs.MasterSyncInProgress {
			downSeconds, ok := info["master_link_down_since_seconds"]
			if !ok {
				state.Error = "Replica with link down but no master_link_down_since_seconds in info"
				return &state
			}
			rs.MasterLinkDownTime, err = strconv.ParseInt(downSeconds, 10, 64)
			rs.MasterLinkDownTime *= 1000
			if err != nil {
				state.Error = err.Error()
				return &state
			}
		}
		replicaOffset, ok := info["slave_repl_offset"]
		if !ok {
			state.Error = "Replica but no slave_repl_offset in info"
			return &state
		}
		rs.ReplicationOffset, err = strconv.ParseInt(replicaOffset, 10, 64)
		if err != nil {
			state.Error = err.Error()
			return &state
		}
		state.ReplicaState = &rs
	}
	state.IsReadOnly, err = node.IsReadOnly(app.ctx)
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	state.IsOffline, err = node.IsOffline(app.ctx)
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	state.IsReplPaused, err = node.IsReplPaused(app.ctx)
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	err = node.RefreshAddrs()
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	state.IP, err = node.GetIP()
	if err != nil {
		state.Error = err.Error()
		return &state
	}
	return &state
}

func (app *App) getShardStateFromDcs() (map[string]*HostState, error) {
	hosts := app.shard.Hosts()
	getter := func(host string) (*HostState, error) {
		var state HostState
		err := app.dcs.Get(dcs.JoinPath(pathHealthPrefix, host), &state)
		if err != nil && err != dcs.ErrNotFound {
			return nil, err
		}
		return &state, nil
	}
	return getHostStatesInParallel(hosts, getter)
}

func (app *App) getShardStateFromDB() (map[string]*HostState, error) {
	hosts := app.shard.Hosts()
	getter := func(host string) (*HostState, error) {
		return app.getHostState(host), nil
	}
	return getHostStatesInParallel(hosts, getter)
}
