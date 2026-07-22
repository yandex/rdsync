package app

import (
	"errors"
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
		if errors.Is(err, dcs.ErrNotFound) {
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
		expected = append(expected, fmt.Sprintf("%s:%d", host, app.config.Valkey.Port))
		for _, ip := range activeNode.GetIPs() {
			expected = append(expected, fmt.Sprintf("%s:%d", ip, app.config.Valkey.Port))
		}
	}

	sort.Strings(expected)

	expectedValue := strings.Join(expected, " ")
	currentValue, err := node.GetQuorumReplicas(app.ctx)
	if err != nil {
		return err
	}

	if currentValue != expectedValue {
		app.logger.Debug().Msgf("Setting quorum replicas to %s on %s", expectedValue, master)
		err, rewriteErr := node.SetQuorumReplicas(app.ctx, expectedValue)
		if err != nil {
			return err
		}
		if rewriteErr != nil {
			app.logger.Error().Str("fqdn", master).Err(rewriteErr).Msg("Unable to rewrite config")
		}
	}

	return nil
}

type activeNodesTransitionOps struct {
	setQuorumReplicas    func([]string) error
	setNumQuorumReplicas func(int) error
	setActiveNodes       func([]string) error
}

func activeNodesSet(nodes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		set[node] = struct{}{}
	}
	return set
}

func activeNodesIntersection(left, right []string) []string {
	rightSet := activeNodesSet(right)
	intersection := make([]string, 0, len(left))
	for _, node := range left {
		if _, ok := rightSet[node]; ok {
			intersection = append(intersection, node)
		}
	}
	sort.Strings(intersection)
	return intersection
}

func activeNodesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSet := activeNodesSet(left)
	for _, node := range right {
		if _, ok := leftSet[node]; !ok {
			return false
		}
	}
	return true
}

func hasAddedActiveNodes(oldActiveNodes, activeNodes []string) bool {
	oldSet := activeNodesSet(oldActiveNodes)
	for _, node := range activeNodes {
		if _, ok := oldSet[node]; !ok {
			return true
		}
	}
	return false
}

func applyActiveNodesTransition(oldActiveNodes, activeNodes []string, actualNumReplicas, expectedNumReplicas int, ops activeNodesTransitionOps) error {
	if activeNodesEqual(oldActiveNodes, activeNodes) {
		if actualNumReplicas < expectedNumReplicas {
			if err := ops.setNumQuorumReplicas(expectedNumReplicas); err != nil {
				return err
			}
		}
		if err := ops.setQuorumReplicas(activeNodes); err != nil {
			return err
		}
		if actualNumReplicas > expectedNumReplicas {
			return ops.setNumQuorumReplicas(expectedNumReplicas)
		}
		return nil
	}

	if hasAddedActiveNodes(oldActiveNodes, activeNodes) || expectedNumReplicas > actualNumReplicas {
		commonActiveNodes := activeNodesIntersection(oldActiveNodes, activeNodes)
		if err := ops.setQuorumReplicas(commonActiveNodes); err != nil {
			return err
		}
		if actualNumReplicas < expectedNumReplicas {
			if err := ops.setNumQuorumReplicas(expectedNumReplicas); err != nil {
				return err
			}
		}
		if err := ops.setActiveNodes(activeNodes); err != nil {
			return err
		}
		if err := ops.setQuorumReplicas(activeNodes); err != nil {
			return err
		}
		if actualNumReplicas > expectedNumReplicas {
			return ops.setNumQuorumReplicas(expectedNumReplicas)
		}
		return nil
	}

	if err := ops.setActiveNodes(activeNodes); err != nil {
		return err
	}
	if err := ops.setQuorumReplicas(activeNodes); err != nil {
		return err
	}
	if actualNumReplicas != expectedNumReplicas {
		return ops.setNumQuorumReplicas(expectedNumReplicas)
	}
	return nil
}

func (app *App) updateActiveNodes(state, stateDcs map[string]*HostState, oldActiveNodes []string, master string) error {
	activeNodes := app.calcActiveNodes(state, stateDcs, oldActiveNodes, master)
	masterNode := app.shard.Get(master)
	actualNumReplicas, err := masterNode.GetNumQuorumReplicas(app.ctx)
	if err != nil {
		return fmt.Errorf("get num quorum replicas on master: %w", err)
	}
	expectedNumReplicas := app.getNumReplicasToWrite(activeNodes)

	ops := activeNodesTransitionOps{
		setQuorumReplicas: func(nodes []string) error {
			if err := app.actualizeQuorumReplicas(master, nodes); err != nil {
				return fmt.Errorf("actualize quorum replicas: %w", err)
			}
			return nil
		},
		setNumQuorumReplicas: func(value int) error {
			app.logger.Info().Msgf("Update active nodes: changing num quorum replicas from %d to %d on master", actualNumReplicas, value)
			err, rewriteErr := masterNode.SetNumQuorumReplicas(app.ctx, value)
			if err != nil {
				return fmt.Errorf("set num quorum replicas on master: %w", err)
			}
			if rewriteErr != nil {
				app.logger.Error().Err(rewriteErr).Msg("Update active nodes: failed to rewrite config on master")
			}
			return nil
		},
		setActiveNodes: func(nodes []string) error {
			if err := app.dcs.Set(pathActiveNodes, nodes); err != nil {
				return fmt.Errorf("update active nodes in dcs: %w", err)
			}
			return nil
		},
	}

	return applyActiveNodesTransition(oldActiveNodes, activeNodes, actualNumReplicas, expectedNumReplicas, ops)
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
					app.logger.Warn().Msgf("Calc active nodes: %s keeps health lock in dcs, keeping active...", host)
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
					app.logger.Warn().Msgf("Calc active nodes: %s is failing, remaining %v", host, app.config.InactivationDelay-failTime)
					activeNodes = append(activeNodes, host)
				}
				continue
			}
			app.logger.Error().Msgf("Calc active nodes: %s is down, deleting from active...", host)
			continue
		} else if !stateDcs[host].IsOffline {
			delete(app.nodeFailTime, host)
		}
		replicaState := node.ReplicaState
		if replicaState == nil {
			app.logger.Warn().Msgf("Calc active nodes: lost master %s", host)
			continue
		}
		if (masterState.PingOk && masterState.PingStable) && !replicates(&masterState, replicaState, host, masterNode, false) {
			app.logger.Error().Msgf("Calc active nodes: %s is not replicating from alive master, deleting from active...", host)
			continue
		}
		activeNodes = append(activeNodes, host)
	}

	sort.Strings(activeNodes)
	return activeNodes
}
