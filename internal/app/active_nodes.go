package app

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

// GetActiveNodes returns a list of active nodes from DCS
func (app *App) GetActiveNodes() ([]string, error) {
	var activeNodes []string
	err := app.dcs.Get(pathActiveNodes, &activeNodes)
	if err != nil {
		if err == dcs.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get active nodes from dcs: %s", err.Error())
	}
	return activeNodes, nil
}

func (app *App) actualizeQuorumReplicas(master string, activeNodes []string) error {
	node := app.shard.Get(master)
	var expected []string

	for _, host := range activeNodes {
		if host == master {
			continue
		}
		activeNode := app.shard.Get(host)
		expected = append(expected, fmt.Sprintf("%s:%d", host, app.config.Redis.Port))
		for _, ip := range activeNode.GetIPs() {
			expected = append(expected, fmt.Sprintf("%s:%d", ip, app.config.Redis.Port))
		}
	}

	sort.Strings(expected)

	expectedValue := strings.Join(expected, " ")
	currentValue, err := node.GetQuorumReplicas(app.ctx)
	if err != nil {
		return err
	}

	if currentValue != expectedValue {
		app.logger.Debug(fmt.Sprintf("Setting quorum replicas to %s on %s", expectedValue, master))
		err, rewriteErr := node.SetQuorumReplicas(app.ctx, expectedValue)
		if err != nil {
			return err
		}
		if rewriteErr != nil {
			app.logger.Error("Unable to rewrite config", "fqdn", master, "error", rewriteErr)
		}
	}

	return nil
}

func (app *App) updateActiveNodes(state, stateDcs map[string]*HostState, oldActiveNodes []string, master string) error {
	activeNodes := app.calcActiveNodes(state, stateDcs, oldActiveNodes, master)

	var addNodes []string

	for _, node := range activeNodes {
		if !slices.Contains(oldActiveNodes, node) {
			addNodes = append(addNodes, node)
		}
	}

	if len(addNodes) > 0 {
		addNodes = append(addNodes, oldActiveNodes...)
		err := app.dcs.Set(pathActiveNodes, addNodes)
		if err != nil {
			app.logger.Error("Update active nodes: failed to update active nodes in dcs", "error", err)
			return err
		}
	}

	err := app.actualizeQuorumReplicas(master, activeNodes)
	if err != nil {
		app.logger.Error("Update active nodes: failed to actualize quorum replicas", "error", err)
		return err
	}

	err = app.dcs.Set(pathActiveNodes, activeNodes)
	if err != nil {
		app.logger.Error("Update active nodes: failed to update active nodes in dcs", "error", err)
		return err
	}
	return nil
}

func (app *App) calcActiveNodes(state, stateDcs map[string]*HostState, oldActiveNodes []string, master string) []string {
	var activeNodes []string
	masterNode := app.shard.Get(master)
	var masterState HostState
	for host, node := range state {
		if host == master {
			activeNodes = append(activeNodes, master)
			if node != nil {
				masterState = *node
			}
			continue
		}
	}
	for host, node := range state {
		if host == master {
			continue
		}
		if !node.PingOk {
			if stateDcs[host].PingOk {
				if slices.Contains(oldActiveNodes, host) {
					app.logger.Warn(fmt.Sprintf("Calc active nodes: %s keeps health lock in dcs, keeping active...", host))
					activeNodes = append(activeNodes, host)
				}
				continue
			}
			if app.nodeFailTime[host].IsZero() {
				app.nodeFailTime[host] = time.Now()
			}
			failTime := time.Since(app.nodeFailTime[host])
			if failTime < app.config.InactivationDelay {
				if slices.Contains(oldActiveNodes, host) {
					app.logger.Warn(fmt.Sprintf("Calc active nodes: %s is failing, remaining %v", host, app.config.InactivationDelay-failTime))
					activeNodes = append(activeNodes, host)
				}
				continue
			}
			app.logger.Error(fmt.Sprintf("Calc active nodes: %s is down, deleting from active...", host))
			continue
		} else if !stateDcs[host].IsOffline {
			delete(app.nodeFailTime, host)
		}
		replicaState := node.ReplicaState
		if replicaState == nil {
			app.logger.Warn(fmt.Sprintf("Calc active nodes: lost master %s", host))
			continue
		}
		if (masterState.PingOk && masterState.PingStable) && !replicates(&masterState, replicaState, host, masterNode, false) {
			app.logger.Error(fmt.Sprintf("Calc active nodes: %s is not replicating from alive master, deleting from active...", host))
			continue
		}
		activeNodes = append(activeNodes, host)
	}

	sort.Strings(activeNodes)
	return activeNodes
}
