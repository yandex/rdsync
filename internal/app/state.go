package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) setStateError(state *HostState, fqdn, message string) {
	app.logger.Error("GetHostState error", "fqdn", fqdn, "error", message)
	state.Error = message
}

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
		app.setStateError(&state, fqdn, err.Error())
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
		app.setStateError(&state, fqdn, "No run_id in info")
		return &state
	}
	state.ReplicationID, ok = info["master_replid"]
	if !ok {
		app.setStateError(&state, fqdn, "No master_replid in info")
		return &state
	}
	state.ReplicationID2, ok = info["master_replid2"]
	if !ok {
		app.setStateError(&state, fqdn, "No master_replid2 in info")
		return &state
	}
	masterOffset, ok := info["master_repl_offset"]
	if !ok {
		app.setStateError(&state, fqdn, "No master_repl_offset in info")
		return &state
	}
	state.MasterReplicationOffset, err = strconv.ParseInt(masterOffset, 10, 64)
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	secondOffset, ok := info["second_repl_offset"]
	if !ok {
		app.setStateError(&state, fqdn, "No second_repl_offset in info")
		return &state
	}
	state.SecondReplicationOffset, err = strconv.ParseInt(secondOffset, 10, 64)
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	replBacklogFirstByte, ok := info["repl_backlog_first_byte_offset"]
	if !ok {
		app.setStateError(&state, fqdn, "No repl_backlog_first_byte_offset in info")
		return &state
	}
	state.ReplicationBacklogStart, err = strconv.ParseInt(replBacklogFirstByte, 10, 64)
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	replBacklogHistlen, ok := info["repl_backlog_histlen"]
	if !ok {
		app.setStateError(&state, fqdn, "No repl_backlog_histlen in info")
		return &state
	}
	state.ReplicationBacklogSize, err = strconv.ParseInt(replBacklogHistlen, 10, 64)
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	role, ok := info["role"]
	if !ok {
		app.setStateError(&state, fqdn, "No role in info")
		return &state
	}
	if role == "master" {
		state.IsMaster = true
		numReplicasStr, ok := info["connected_slaves"]
		if !ok {
			app.setStateError(&state, fqdn, "Master has no connected_slaves in info")
			return &state
		}
		numReplicas, err := strconv.ParseInt(numReplicasStr, 10, 64)
		if err != nil {
			app.setStateError(&state, fqdn, err.Error())
			return &state
		}
		var i int64
		for i < numReplicas {
			replicaID := fmt.Sprintf("slave%d", i)
			replicaValue, ok := info[replicaID]
			if !ok {
				app.logger.Warn(fmt.Sprintf("Master has no %s in info but connected_slaves is %d", replicaID, numReplicas), "fqdn", fqdn)
				continue
			}
			// ip is first value in slaveN info
			start := strings.Index(replicaValue, "=")
			end := strings.Index(replicaValue, ",")
			state.ConnectedReplicas = append(state.ConnectedReplicas, replicaValue[start+1:end])
			i++
		}
	} else {
		state.IsMaster = false
		rs := ReplicaState{}
		rs.MasterHost, ok = info["master_host"]
		if !ok {
			app.setStateError(&state, fqdn, "Replica but no master_host in info")
			return &state
		}
		linkState, ok := info["master_link_status"]
		if !ok {
			app.setStateError(&state, fqdn, "Replica but no master_link_status in info")
			return &state
		}
		rs.MasterLinkState = (linkState == "up")
		syncInProgress, ok := info["master_sync_in_progress"]
		if !ok {
			app.setStateError(&state, fqdn, "Replica but no master_sync_in_progress in info")
			return &state
		}
		rs.MasterSyncInProgress = (syncInProgress != "0")
		if !rs.MasterLinkState && !rs.MasterSyncInProgress {
			downSeconds, ok := info["master_link_down_since_seconds"]
			if !ok {
				app.setStateError(&state, fqdn, "Replica with link down but no master_link_down_since_seconds in info")
				return &state
			}
			rs.MasterLinkDownTime, err = strconv.ParseInt(downSeconds, 10, 64)
			rs.MasterLinkDownTime *= 1000
			if err != nil {
				app.setStateError(&state, fqdn, err.Error())
				return &state
			}
		}
		replicaOffset, ok := info["slave_repl_offset"]
		if !ok {
			app.setStateError(&state, fqdn, "Replica but no slave_repl_offset in info")
			return &state
		}
		rs.ReplicationOffset, err = strconv.ParseInt(replicaOffset, 10, 64)
		if err != nil {
			app.setStateError(&state, fqdn, err.Error())
			return &state
		}
		state.ReplicaState = &rs
	}
	state.MinReplicasToWrite, err = node.GetMinReplicasToWrite(app.ctx)
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	state.IsReadOnly = node.IsReadOnly(state.MinReplicasToWrite)
	state.IsOffline, err = node.IsOffline(app.ctx)
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	state.IsReplPaused, err = node.IsReplPaused(app.ctx)
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	err = node.RefreshAddrs()
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
		return &state
	}
	state.IP, err = node.GetIP()
	if err != nil {
		app.setStateError(&state, fqdn, err.Error())
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
