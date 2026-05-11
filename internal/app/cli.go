package app

import (
	"bufio"
	"context"
	"fmt"

	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/yandex/rdsync/internal/dcs"
	"github.com/yandex/rdsync/internal/valkey"
)

// CliInfo prints DCS-based shard state to stdout
func (app *App) CliInfo(verbose bool) int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	app.dcs.Initialize()
	defer app.dcs.Close()

	app.shard = valkey.NewShard(app.config, app.logger, app.dcs)
	defer app.shard.Close()
	if err := app.shard.UpdateHostsInfo(); err != nil {
		app.logger.Error().Err(err).Msg("Unable to update hosts info")
		return 1
	}

	var tree any
	if !verbose {
		data := make(map[string]any)

		haNodes, err := app.shard.GetShardHostsFromDcs()
		if err != nil {
			app.logger.Error().Err(err).Msg("Failed to get hosts")
			return 1
		}
		data[pathHANodes] = haNodes

		activeNodes, err := app.GetActiveNodes()
		if err != nil {
			app.logger.Error().Err(err).Msg("Failed to get active nodes")
			return 1
		}
		sort.Strings(activeNodes)
		data[pathActiveNodes] = activeNodes

		shardState, err := app.getShardStateFromDcs()
		if err != nil {
			app.logger.Error().Err(err).Msg("Failed to get shard state")
			return 1
		}
		health := make(map[string]any)
		for host, state := range shardState {
			health[host] = state.String()
		}
		data[pathHealthPrefix] = health

		for _, path := range []string{pathLastSwitch, pathCurrentSwitch, pathLastRejectedSwitch} {
			var switchover Switchover
			err = app.dcs.Get(path, &switchover)
			if err == nil {
				data[path] = switchover.String()
			} else if err != dcs.ErrNotFound {
				app.logger.Error().Err(err).Msgf("Failed to get %s", path)
				return 1
			}
		}

		var maintenance Maintenance
		err = app.dcs.Get(pathMaintenance, &maintenance)
		if err == nil {
			data[pathMaintenance] = maintenance.String()
		} else if err != dcs.ErrNotFound {
			app.logger.Error().Err(err).Msgf("Failed to get %s", pathMaintenance)
			return 1
		}

		var poisonPill PoisonPill
		err = app.dcs.Get(pathPoisonPill, &poisonPill)
		if err == nil {
			data[pathPoisonPill] = poisonPill.String()
		} else if err != dcs.ErrNotFound {
			app.logger.Error().Err(err).Msgf("Failed to get %s", pathPoisonPill)
			return 1
		}

		var manager dcs.LockOwner
		err = app.dcs.Get(pathManagerLock, &manager)
		if err != nil && err != dcs.ErrNotFound {
			app.logger.Error().Err(err).Msgf("Failed to get %s", pathManagerLock)
			return 1
		}
		data[pathManagerLock] = manager.Hostname

		var master string
		err = app.dcs.Get(pathMasterNode, &master)
		if err != nil && err != dcs.ErrNotFound {
			app.logger.Error().Err(err).Msgf("Failed to get %s", pathMasterNode)
			return 1
		}
		data[pathMasterNode] = master
		tree = data
	} else {
		tree, err = app.dcs.GetTree("")
		if err != nil {
			app.logger.Error().Err(err).Msg("Failed to get tree")
			return 1
		}
	}
	data, err := yaml.Marshal(tree)
	if err != nil {
		app.logger.Error().Err(err).Msg("failed to marshal yaml")
		return 1
	}
	fmt.Print(string(data))
	return 0
}

// CliState prints state of the shard to the stdout
func (app *App) CliState(verbose bool) int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()
	app.shard = valkey.NewShard(app.config, app.logger, app.dcs)
	defer app.shard.Close()

	if err := app.shard.UpdateHostsInfo(); err != nil {
		app.logger.Error().Err(err).Msg("Unable to update hosts info")
		return 1
	}

	shardState, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error().Err(err).Msg("Failed to get state")
		return 1
	}
	var tree any
	if !verbose {
		shardStateStrings := make(map[string]string)
		for host, state := range shardState {
			shardStateStrings[host] = state.String()
		}
		tree = shardStateStrings
	} else {
		tree = shardState
	}
	data, err := yaml.Marshal(tree)
	if err != nil {
		app.logger.Error().Err(err).Msg("Failed to marshal yaml")
		return 1
	}
	fmt.Print(string(data))
	return 0
}

func matchPrefix(hosts []string, prefix string) []string {
	matched := make([]string, 0)
	for _, host := range hosts {
		if strings.HasPrefix(host, prefix) {
			matched = append(matched, host)
		}
	}
	return matched
}

// CliSwitch performs manual switch-over of the master node
func (app *App) CliSwitch(switchFrom, switchTo string, waitTimeout time.Duration, switchForce bool) int {
	if switchFrom == "" && switchTo == "" {
		app.logger.Error().Msg("Either --from or --to should be set")
		return 1
	}
	if switchFrom != "" && switchTo != "" {
		app.logger.Error().Msg("Option --from and --to can't be used at the same time")
		return 1
	}
	if switchFrom != "" && switchForce {
		app.logger.Error().Msg("Option --from and --force can't be used at the same time")
		return 1
	}
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()
	app.shard = valkey.NewShard(app.config, app.logger, app.dcs)
	defer app.shard.Close()

	if err := app.shard.UpdateHostsInfo(); err != nil {
		app.logger.Error().Err(err).Msg("Unable to update hosts info")
		return 1
	}

	if len(app.shard.Hosts()) == 1 {
		app.logger.Info().Msg("switchover makes no sense on single node shard")
		fmt.Println("switchover done")
		return 0
	}

	var fromHost, toHost string

	var currentMaster string
	if err := app.dcs.Get(pathMasterNode, &currentMaster); err != nil {
		app.logger.Error().Err(err).Msg("Failed to get current master")
		return 1
	}
	activeNodes, err := app.GetActiveNodes()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to get active nodes")
		return 1
	}

	if switchTo != "" {
		desired := matchPrefix(app.shard.Hosts(), switchTo)
		if len(desired) == 0 {
			app.logger.Error().Msgf("No nodes match '%s'", switchTo)
			return 1
		}
		if len(desired) > 1 {
			app.logger.Error().Msgf("More than one node matches '%s': %s", switchTo, desired)
			return 1
		}
		toHost = desired[0]
		if toHost == currentMaster {
			app.logger.Info().Msgf("Master is already on %s, skipping...", toHost)
			fmt.Println("switchover done")
			return 0
		}
		if !slices.Contains(activeNodes, toHost) {
			app.logger.Error().Msgf("%s is not active, can't switch to it", toHost)
			return 1
		}
	} else {
		notDesired := matchPrefix(app.shard.Hosts(), switchFrom)
		if len(notDesired) == 0 {
			app.logger.Error().Msgf("No HA-nodes matches '%s'", switchFrom)
			return 1
		}
		if !slices.Contains(notDesired, currentMaster) {
			app.logger.Info().Msgf("Master is already not on %s, skipping...", notDesired)
			fmt.Println("switchover done")
			return 0
		}
		var candidates []string
		for _, node := range activeNodes {
			if !slices.Contains(notDesired, node) {
				candidates = append(candidates, node)
			}
		}
		if len(candidates) == 0 {
			app.logger.Error().Msgf("There are no active nodes, not matching '%s'", switchFrom)
			return 1
		}
		if len(notDesired) == 1 {
			fromHost = notDesired[0]
		} else {
			states, err := app.getShardStateFromDB()
			if err != nil {
				app.logger.Error().Err(err).Msg("No actual shard state")
				return 1
			}
			toHost, err = app.getMostDesirableNode(states, switchFrom)
			if err != nil {
				app.logger.Error().Err(err).Msg("No desirable node")
				return 1
			}
		}
	}

	var switchover Switchover
	err = app.dcs.Get(pathCurrentSwitch, &switchover)
	if err == nil {
		app.logger.Error().Msgf("Another switchover in progress %v", switchover)
		return 2
	}
	if err != dcs.ErrNotFound {
		app.logger.Error().Err(err).Msg("Unable to get current switchover status")
		return 2
	}

	switchover.From = fromHost
	switchover.To = toHost
	switchover.InitiatedBy = app.config.Hostname
	switchover.InitiatedAt = time.Now()
	switchover.Cause = CauseManual
	if switchForce {
		switchover.RunCount = 1
		err = app.dcs.Set(pathActiveNodes, []string{toHost})
		if err != nil {
			app.logger.Error().Msg("Unable to update active nodes")
			return 1
		}
	}

	err = app.dcs.Create(pathCurrentSwitch, switchover)
	if err == dcs.ErrExists {
		app.logger.Error().Msg("Another switchover in progress")
		return 2
	}
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to create switchover in dcs")
		return 1
	}
	// wait for switchover to complete
	if waitTimeout > 0 {
		var lastSwitchover Switchover
		waitCtx, cancel := context.WithTimeout(app.ctx, waitTimeout)
		defer cancel()
		ticker := time.NewTicker(time.Second)
	Out:
		for {
			select {
			case <-ticker.C:
				lastSwitchover = app.getLastSwitchover()
				if lastSwitchover.InitiatedBy == switchover.InitiatedBy && lastSwitchover.InitiatedAt.Unix() == switchover.InitiatedAt.Unix() {
					break Out
				} else {
					lastSwitchover = Switchover{}
				}
			case <-waitCtx.Done():
				break Out
			}
		}
		if lastSwitchover.Result == nil {
			app.logger.Error().Msg("Switchover did not finish until deadline")
			return 1
		} else if !lastSwitchover.Result.Ok {
			app.logger.Error().Msg("Could not wait for switchover to complete because of errors")
			return 1
		}
		fmt.Println("switchover done")
	} else {
		fmt.Println("switchover scheduled")
	}
	return 0
}

// CliEnableMaintenance enables maintenance mode
func (app *App) CliEnableMaintenance(waitTimeout time.Duration) int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()

	maintenance := &Maintenance{
		InitiatedBy: app.config.Hostname,
		InitiatedAt: time.Now(),
	}
	err = app.dcs.Create(pathMaintenance, maintenance)
	if err != nil && err != dcs.ErrExists {
		app.logger.Error().Err(err).Msg("Unable to create maintenance path in dcs")
		return 1
	}
	if waitTimeout > 0 {
		waitCtx, cancel := context.WithTimeout(app.ctx, waitTimeout)
		defer cancel()
		ticker := time.NewTicker(time.Second)
	Out:
		for {
			select {
			case <-ticker.C:
				err = app.dcs.Get(pathMaintenance, maintenance)
				if err != nil {
					app.logger.Error().Err(err).Msg("Unable to get maintenance status from dcs")
				}
				if maintenance.RdSyncPaused {
					break Out
				}
			case <-waitCtx.Done():
				break Out
			}
		}
		if !maintenance.RdSyncPaused {
			app.logger.Error().Msg("Rdsync did not enter maintenance within timeout")
			return 1
		}
		fmt.Println("maintenance enabled")
	} else {
		fmt.Println("maintenance scheduled")
	}
	return 0
}

// CliDisableMaintenance disables maintenance mode
func (app *App) CliDisableMaintenance(waitTimeout time.Duration) int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()

	maintenance := &Maintenance{}
	err = app.dcs.Get(pathMaintenance, maintenance)
	if err == dcs.ErrNotFound {
		fmt.Println("maintenance disabled")
		return 0
	} else if err != nil {
		app.logger.Error().Err(err).Msg("Unable to get maintenance status from dcs")
		return 1
	}
	maintenance.ShouldLeave = true
	err = app.dcs.Set(pathMaintenance, maintenance)
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to update maintenance in dcs")
		return 1
	}
	if waitTimeout > 0 {
		waitCtx, cancel := context.WithTimeout(app.ctx, waitTimeout)
		defer cancel()
		ticker := time.NewTicker(time.Second)
	Out:
		for {
			select {
			case <-ticker.C:
				err = app.dcs.Get(pathMaintenance, maintenance)
				if err == dcs.ErrNotFound {
					maintenance = nil
					break Out
				}
				if err != nil {
					app.logger.Error().Err(err).Msg("Unable to get maintenance status from dcs")
				}
			case <-waitCtx.Done():
				break Out
			}
		}
		if maintenance != nil {
			app.logger.Error().Msg("Rdsync did not leave maintenance within timeout")
			return 1
		}
		fmt.Println("maintenance disabled")
	} else {
		fmt.Println("maintenance disable scheduled")
	}
	return 0
}

// CliGetMaintenance prints on/off depending on current maintenance status
func (app *App) CliGetMaintenance() int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()

	var maintenance Maintenance
	err = app.dcs.Get(pathMaintenance, &maintenance)
	switch err {
	case nil:
		if maintenance.RdSyncPaused {
			fmt.Println("on")
		} else {
			fmt.Println("scheduled")
		}
		return 0
	case dcs.ErrNotFound:
		fmt.Println("off")
		return 0
	default:
		app.logger.Error().Err(err).Msg("Unable to get maintenance status")
		return 1
	}
}

// CliAbort cleans switchover node from DCS
func (app *App) CliAbort() int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()

	err = app.dcs.Get(pathCurrentSwitch, new(Switchover))
	if err == dcs.ErrNotFound {
		fmt.Println("no active switchover")
		return 0
	}
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to get switchover status")
		return 1
	}

	const phrase = "yes, abort switch"
	fmt.Printf("please, confirm aborting switchover by typing '%s'\n", phrase)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to parse response")
		return 1
	}
	if strings.TrimSpace(response) != phrase {
		fmt.Printf("doesn't match, do nothing")
		return 1
	}

	err = app.dcs.Delete(pathCurrentSwitch)
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to remove switchover path from dcs")
		return 1
	}

	fmt.Printf("switchover aborted\n")
	return 0
}

// CliHostList prints list of hosts from dcs
func (app *App) CliHostList() int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	app.dcs.Initialize()
	defer app.dcs.Close()

	app.shard = valkey.NewShard(app.config, app.logger, app.dcs)
	defer app.shard.Close()

	data := make(map[string]any)

	hosts, err := app.shard.GetShardHostsFromDcs()
	if err != nil {
		app.logger.Error().Err(err).Msg("Failed to get hosts")
		return 1
	}
	sort.Strings(hosts)
	data[pathHANodes] = hosts

	out, err := yaml.Marshal(data)
	if err != nil {
		app.logger.Error().Err(err).Msg("Failed to marshal yaml")
		return 1
	}
	fmt.Print(string(out))
	return 0
}

// CliHostAdd add hosts to the list of hosts in dcs
func (app *App) CliHostAdd(host string, priority *int, dryRun bool, skipValkeyCheck bool) int {
	if priority != nil && *priority < 0 {
		app.logger.Error().Msgf("Priority must be >= 0. Got: %d", *priority)
		return 1
	}

	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()

	app.shard = valkey.NewShard(app.config, app.logger, app.dcs)
	defer app.shard.Close()

	// root path probably does not exist
	err = app.dcs.Create(dcs.JoinPath(pathHANodes), nil)
	if err != nil && err != dcs.ErrExists {
		return 1
	}

	if !skipValkeyCheck {
		node, err := valkey.NewNode(app.config, app.logger, host)
		if err != nil {
			app.logger.Error().Err(err).Msgf("Failed to check connection to %s, can't tell if it's alive", host)
			return 1
		}
		defer node.Close()
		_, _, _, _, _, err = node.GetState(app.ctx)
		if err != nil {
			app.logger.Error().Err(err).Msgf("Node %s is dead", host)
			return 1
		}
	}

	if !dryRun && priority == nil {
		err = app.dcs.Set(dcs.JoinPath(pathHANodes, host), *valkey.DefaultNodeConfiguration())
		if err != nil && err != dcs.ErrExists {
			app.logger.Error().Err(err).Msgf("Unable to create dcs path for %s", host)
			return 1
		}
	}

	changes, err := app.processPriority(priority, dryRun, host)
	if err != nil {
		return 1
	}

	if dryRun {
		if !changes {
			fmt.Println("dry run finished: no changes detected")
			return 0
		}
		return 2
	}

	fmt.Println("host has been added")
	return 0
}

// CliHostRemove removes host from the list of hosts in dcs
func (app *App) CliHostRemove(host string) int {
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.Initialize()

	err = app.dcs.Delete(dcs.JoinPath(pathHANodes, host))
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error().Err(err).Msgf("Unable to delete dcs path for %s", host)
		return 1
	}
	fmt.Println("host has been removed")
	return 0
}

func (app *App) processPriority(priority *int, dryRun bool, host string) (changes bool, err error) {
	targetConf := valkey.DefaultNodeConfiguration()
	if priority != nil {
		targetConf.Priority = *priority
	}
	if dryRun {
		hosts, err := app.shard.GetShardHostsFromDcs()
		if err != nil {
			return false, err
		}
		exists := slices.Contains(hosts, host)
		if !exists {
			fmt.Print("dry run: node can be created\n")
			return true, nil
		}
		nc, err := app.shard.GetNodeConfiguration(host)
		if err != nil {
			return false, err
		}
		if nc.Priority == targetConf.Priority {
			fmt.Printf("dry run: node already has priority %d set\n", priority)
			return false, nil
		}
		fmt.Printf("dry run: node priority can be set to %d (current priority %d)\n", targetConf.Priority, nc.Priority)
		return true, nil
	}

	err = app.dcs.Set(dcs.JoinPath(pathHANodes, host), targetConf)
	if err != nil && err != dcs.ErrExists {
		return false, err
	}

	return true, nil
}
