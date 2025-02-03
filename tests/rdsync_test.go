package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/go-zookeeper/zk"
	client "github.com/valkey-io/valkey-go"

	"github.com/yandex/rdsync/internal/dcs"
	"github.com/yandex/rdsync/tests/testutil"
	"github.com/yandex/rdsync/tests/testutil/matchers"
)

const (
	zkName                      = "zoo"
	zkPort                      = 2181
	zkConnectTimeout            = 5 * time.Second
	valkeyName                  = "valkey"
	valkeyPort                  = 6379
	senticachePort              = 26379
	valkeyPassword              = "functestpassword"
	valkeyConnectTimeout        = 30 * time.Second
	valkeyInitialConnectTimeout = 2 * time.Minute
	valkeyCmdTimeout            = 15 * time.Second
	testUser                    = "testuser"
	testPassword                = "testpassword123"
)

var valkeyLogsToSave = map[string]string{
	"/var/log/supervisor.log":        "supervisor.log",
	"/var/log/rdsync.log":            "rdsync.log",
	"/var/log/valkey/server.log":     "valkey.log",
	"/var/log/valkey/senticache.log": "senticache.log",
}

var zkLogsToSave = map[string]string{
	"/var/log/zookeeper/zookeeper.log": "zookeeper.log",
}

type noLogger struct{}

func (noLogger) Printf(string, ...interface{}) {}

func (noLogger) Print(...interface{}) {}

type testContext struct {
	variables           map[string]interface{}
	templateErr         error
	composer            testutil.Composer
	composerEnv         []string
	zk                  *zk.Conn
	conns               map[string]client.Client
	senticaches         map[string]client.Client
	zkQueryResult       string
	valkeyCmdResult     string
	senticacheCmdResult string
	commandRetcode      int
	commandOutput       string
	acl                 []zk.ACL
}

func newTestContext() (*testContext, error) {
	var err error
	tctx := new(testContext)
	tctx.composer, err = testutil.NewDockerComposer("rdsync", "images/docker-compose.yaml")
	if err != nil {
		return nil, err
	}
	tctx.conns = make(map[string]client.Client)
	tctx.senticaches = make(map[string]client.Client)
	tctx.acl = zk.DigestACL(zk.PermAll, testUser, testPassword)
	return tctx, nil
}

func (tctx *testContext) saveLogs(scenario string) error {
	for _, service := range tctx.composer.Services() {
		var logsToSave map[string]string
		switch {
		case strings.HasPrefix(service, valkeyName):
			logsToSave = valkeyLogsToSave
		case strings.HasPrefix(service, zkName):
			logsToSave = zkLogsToSave
		default:
			continue
		}
		logdir := filepath.Join("logs", scenario, service)
		err := os.MkdirAll(logdir, 0755)
		if err != nil {
			return err
		}
		for remotePath, localPath := range logsToSave {
			remoteFile, err := tctx.composer.GetFile(service, remotePath)
			if err != nil {
				return err
			}
			defer func() { _ = remoteFile.Close() }()
			localFile, err := os.OpenFile(filepath.Join(logdir, localPath), os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				return err
			}
			defer func() { _ = localFile.Close() }()
			_, err = io.Copy(localFile, remoteFile)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (tctx *testContext) templateStep(step *godog.Step) error {
	var err error
	step.Text, err = tctx.templateString(step.Text)
	if err != nil {
		return err
	}

	if step.Argument != nil {
		if step.Argument.DocString != nil {
			newContent, err := tctx.templateString(step.Argument.DocString.Content)
			if err != nil {
				return err
			}
			step.Argument.DocString.Content = newContent
		}
		if step.Argument.DataTable != nil {
			if step.Argument.DataTable.Rows != nil {
				for _, row := range step.Argument.DataTable.Rows {
					for _, cell := range row.Cells {
						cell.Value, err = tctx.templateString(cell.Value)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func (tctx *testContext) templateString(data string) (string, error) {
	if !strings.Contains(data, "{{") {
		return data, nil
	}
	tpl, err := template.New(data).Parse(data)
	if err != nil {
		return data, err
	}
	var res strings.Builder
	err = tpl.Execute(&res, tctx.variables)
	if err != nil {
		return data, err
	}
	return res.String(), nil
}

func (tctx *testContext) cleanup() {
	if tctx.zk != nil {
		tctx.zk.Close()
		tctx.zk = nil
	}
	for _, conn := range tctx.conns {
		conn.Close()
	}
	tctx.conns = make(map[string]client.Client)
	for _, conn := range tctx.senticaches {
		conn.Close()
	}
	tctx.senticaches = make(map[string]client.Client)
	if err := tctx.composer.Down(); err != nil {
		log.Printf("failed to tear down compose: %s", err)
	}

	tctx.variables = make(map[string]interface{})
	tctx.composerEnv = make([]string, 0)
	tctx.zkQueryResult = ""
	tctx.valkeyCmdResult = ""
	tctx.senticacheCmdResult = ""
	tctx.commandRetcode = 0
	tctx.commandOutput = ""
}

func (tctx *testContext) connectZookeeper(addrs []string, timeout time.Duration) (*zk.Conn, error) {
	conn, _, err := zk.Connect(addrs, timeout, zk.WithLogger(noLogger{}))
	if err != nil {
		return nil, err
	}
	err = conn.AddAuth("digest", []byte(fmt.Sprintf("%s:%s", testUser, testPassword)))
	if err != nil {
		return nil, err
	}
	testutil.Retry(func() bool {
		_, _, err = conn.Get("/")
		return err == nil
	}, timeout, time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to ping zookeeper within %s: %s", timeout, err)
	}
	return conn, nil
}

func (tctx *testContext) connectValkey(addr string, timeout time.Duration) (client.Client, error) {
	opts := client.ClientOption{
		InitAddress:           []string{addr},
		ForceSingleClient:     true,
		DisableAutoPipelining: true,
		DisableCache:          true,
		BlockingPoolMinSize:   1,
		BlockingPoolCleanup:   time.Second,
		Password:              valkeyPassword,
		Dialer:                net.Dialer{Timeout: time.Second},
		ConnWriteTimeout:      time.Second,
	}
	var conn client.Client
	var err error
	testutil.Retry(func() bool {
		conn, err = client.NewClient(opts)
		if err != nil {
			conn = nil
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), valkeyCmdTimeout)
		defer cancel()
		err = conn.Do(ctx, conn.B().Ping().Build()).Error()
		return err == nil
	}, timeout, time.Second)
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, err
	}
	return conn, nil
}

func (tctx *testContext) connectSenticache(addr string, timeout time.Duration) (client.Client, error) {
	opts := client.ClientOption{
		InitAddress:           []string{addr},
		ForceSingleClient:     true,
		DisableAutoPipelining: true,
		DisableCache:          true,
		BlockingPoolMinSize:   1,
		BlockingPoolCleanup:   time.Second,
		Dialer:                net.Dialer{Timeout: time.Second},
		ConnWriteTimeout:      time.Second,
	}
	var conn client.Client
	var err error
	testutil.Retry(func() bool {
		conn, err = client.NewClient(opts)
		if err != nil {
			conn = nil
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), valkeyCmdTimeout)
		defer cancel()
		err = conn.Do(ctx, conn.B().Ping().Build()).Error()
		return err == nil
	}, timeout, time.Second)
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, err
	}
	return conn, nil
}

func (tctx *testContext) getValkeyConnection(host string) (client.Client, error) {
	conn, ok := tctx.conns[host]
	if !ok {
		return nil, fmt.Errorf("valkey %s is not in our host list", host)
	}
	err := conn.Do(context.Background(), conn.B().Ping().Build()).Error()
	if err == nil {
		return conn, nil
	}
	addr, err := tctx.composer.GetAddr(host, valkeyPort)
	if err != nil {
		return nil, fmt.Errorf("failed to get valkey addr %s: %s", host, err)
	}
	conn, err = tctx.connectValkey(addr, valkeyConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to valkey %s: %s", host, err)
	}
	tctx.conns[host] = conn
	return conn, nil
}

func (tctx *testContext) getSenticacheConnection(host string) (client.Client, error) {
	conn, ok := tctx.senticaches[host]
	if !ok {
		return nil, fmt.Errorf("senticache %s is not in our host list", host)
	}
	err := conn.Do(context.Background(), conn.B().Ping().Build()).Error()
	if err == nil {
		return conn, nil
	}
	addr, err := tctx.composer.GetAddr(host, senticachePort)
	if err != nil {
		return nil, fmt.Errorf("failed to get senticache addr %s: %s", host, err)
	}
	conn, err = tctx.connectSenticache(addr, valkeyConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to senticache %s: %s", host, err)
	}
	tctx.senticaches[host] = conn
	return conn, nil
}

func (tctx *testContext) runValkeyCmd(host string, cmd []string) (string, error) {
	conn, err := tctx.getValkeyConnection(host)
	if err != nil {
		return "", err
	}

	tctx.valkeyCmdResult = ""
	ctx, cancel := context.WithTimeout(context.Background(), valkeyCmdTimeout)
	defer cancel()
	result := conn.Do(ctx, conn.B().Arbitrary(cmd...).Build())

	err = result.Error()
	if err != nil {
		tctx.valkeyCmdResult = err.Error()
	} else {
		message, err := result.ToMessage()
		if err != nil {
			tctx.valkeyCmdResult = err.Error()
			return tctx.valkeyCmdResult, err
		}
		if message.IsMap() {
			strMap, err := message.AsStrMap()
			if err != nil {
				tctx.valkeyCmdResult = err.Error()
			} else {
				var pairs []string
				for k, v := range strMap {
					pairs = append(pairs, k+" "+v)
				}
				tctx.valkeyCmdResult = strings.Join(pairs, " ")
			}
		} else if message.IsArray() {
			strSlice, err := message.AsStrSlice()
			if err != nil {
				tctx.valkeyCmdResult = err.Error()
			} else {
				tctx.valkeyCmdResult = strings.Join(strSlice, " ")
			}
		} else {
			tctx.valkeyCmdResult = message.String()
		}
	}

	return tctx.valkeyCmdResult, err
}

func (tctx *testContext) runSenticacheCmd(host string, cmd []string) (string, error) {
	conn, err := tctx.getSenticacheConnection(host)
	if err != nil {
		return "", err
	}

	tctx.senticacheCmdResult = ""
	ctx, cancel := context.WithTimeout(context.Background(), valkeyCmdTimeout)
	defer cancel()
	result := conn.Do(ctx, conn.B().Arbitrary(cmd...).Build())

	err = result.Error()
	if err != nil {
		tctx.senticacheCmdResult = err.Error()
	} else {
		tctx.senticacheCmdResult = result.String()
	}

	return tctx.senticacheCmdResult, err
}

func (tctx *testContext) baseShardIsUpAndRunning() error {
	err := tctx.composer.Up(tctx.composerEnv)
	if err != nil {
		return fmt.Errorf("failed to setup compose cluster: %s", err)
	}

	// check zookeepers
	var zkAddrs []string
	testutil.Retry(func() bool {
		zkAddrs = make([]string, 0)
		for _, service := range tctx.composer.Services() {
			if strings.HasPrefix(service, zkName) {
				addr, err2 := tctx.composer.GetAddr(service, zkPort)
				if err2 != nil {
					err = fmt.Errorf("failed to get zookeeper addr %s: %s", service, err2)
					return false
				}
				zkAddrs = append(zkAddrs, addr)
			}
		}

		tctx.zk, err = tctx.connectZookeeper(zkAddrs, zkConnectTimeout)
		return err == nil
	}, time.Minute*5, time.Second)

	if err != nil {
		return fmt.Errorf("failed to connect to zookeeper %s: %s", zkAddrs, err)
	}

	err = tctx.composer.RunCommandAtHosts("/var/lib/dist/base/generate_certs.sh && supervisorctl restart rdsync",
		"valkey",
		time.Minute)
	if err != nil {
		return fmt.Errorf("failed to generate certs in valkey hosts: %s", err)
	}

	if err = tctx.createZookeeperNode("/test"); err != nil {
		return fmt.Errorf("failed to create namespace zk node due %s", err)
	}
	if err = tctx.createZookeeperNode(dcs.JoinPath("/test", dcs.PathHANodesPrefix)); err != nil {
		return fmt.Errorf("failed to create path prefix zk node due %s", err)
	}

	// prepare valkey nodes
	for _, service := range tctx.composer.Services() {
		if strings.HasPrefix(service, valkeyName) {
			if err = tctx.createZookeeperNode(dcs.JoinPath("/test", dcs.PathHANodesPrefix, service)); err != nil {
				return fmt.Errorf("failed to create %s zk node due %s", service, err)
			}
		}
	}
	return nil
}

func (tctx *testContext) stepClusteredShardIsUpAndRunning() error {
	err := tctx.baseShardIsUpAndRunning()
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("valkey1", "setup_cluster.sh", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("valkey2", "setup_cluster.sh valkey1", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("valkey3", "setup_cluster.sh valkey1", 1*time.Minute)
	if err != nil {
		return err
	}

	// check valkey nodes
	for _, service := range tctx.composer.Services() {
		if strings.HasPrefix(service, valkeyName) {
			addr, err := tctx.composer.GetAddr(service, valkeyPort)
			if err != nil {
				return fmt.Errorf("failed to get valkey addr %s: %s", service, err)
			}
			conn, err := tctx.connectValkey(addr, valkeyInitialConnectTimeout)
			if err != nil {
				return fmt.Errorf("failed to connect to valkey %s: %s", service, err)
			}
			tctx.conns[service] = conn
		}
	}
	return nil
}

func (tctx *testContext) stepSentinelShardIsUpAndRunning() error {
	err := tctx.baseShardIsUpAndRunning()
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("valkey1", "setup_sentinel.sh", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("valkey2", "setup_sentinel.sh valkey1", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("valkey3", "setup_sentinel.sh valkey1", 1*time.Minute)
	if err != nil {
		return err
	}
	// check valkey nodes
	for _, service := range tctx.composer.Services() {
		if strings.HasPrefix(service, valkeyName) {
			addr, err := tctx.composer.GetAddr(service, valkeyPort)
			if err != nil {
				return fmt.Errorf("failed to get valkey addr %s: %s", service, err)
			}
			conn, err := tctx.connectValkey(addr, valkeyInitialConnectTimeout)
			if err != nil {
				return fmt.Errorf("failed to connect to valkey %s: %s", service, err)
			}
			tctx.conns[service] = conn
			saddr, err2 := tctx.composer.GetAddr(service, senticachePort)
			if err2 != nil {
				return fmt.Errorf("failed to get senticache addr %s: %s", service, err2)
			}
			sconn, err2 := tctx.connectSenticache(saddr, valkeyInitialConnectTimeout)
			if err2 != nil {
				return fmt.Errorf("failed to connect to senticache %s: %s", service, err2)
			}
			tctx.senticaches[service] = sconn
		}
	}
	return nil
}

func (tctx *testContext) stepPersistenceDisabled() error {
	for _, service := range tctx.composer.Services() {
		if strings.HasPrefix(service, valkeyName) {
			_, _, err := tctx.composer.RunCommand(service, "sed -i /OnReplicas/d /etc/rdsync.yaml", 10*time.Second)
			if err != nil {
				return err
			}
			_, _, err = tctx.composer.RunCommand(service, "supervisorctl restart rdsync", 10*time.Second)
			if err != nil {
				return err
			}
			_, err = tctx.runValkeyCmd(service, []string{"CONFIG", "SET", "appendonly", "no"})
			if err != nil {
				return err
			}
			_, err = tctx.runValkeyCmd(service, []string{"CONFIG", "SET", "save", ""})
			if err != nil {
				return err
			}
			_, _, err = tctx.composer.RunCommand(service, "echo 'appendonly no' >> /etc/valkey/valkey.conf", 10*time.Second)
			if err != nil {
				return err
			}
			_, _, err = tctx.composer.RunCommand(service, "echo 'save \\'\\'' >> /etc/valkey/valkey.conf", 10*time.Second)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (tctx *testContext) stepHostIsStopped(host string) error {
	return tctx.composer.Stop(host)
}

func (tctx *testContext) stepHostIsDetachedFromTheNetwork(host string) error {
	return tctx.composer.DetachFromNet(host)
}

func (tctx *testContext) stepHostIsStarted(host string) error {
	return tctx.composer.Start(host)
}

func (tctx *testContext) stepHostIsAttachedToTheNetwork(host string) error {
	return tctx.composer.AttachToNet(host)
}

func (tctx *testContext) stepPortOnHostIsBlocked(port int, host string) error {
	return tctx.composer.BlockPort(host, port)
}

func (tctx *testContext) stepPortOnHostIsUnBlocked(port int, host string) error {
	return tctx.composer.UnBlockPort(host, port)
}

func (tctx *testContext) stepHostIsAdded(host string) error {
	err := tctx.composer.Start(host)
	if err != nil {
		return err
	}
	return tctx.createZookeeperNode(dcs.JoinPath("/test", dcs.PathHANodesPrefix, host))
}

func (tctx *testContext) stepHostIsDeleted(host string) error {
	err := tctx.composer.Stop(host)
	if err != nil {
		return err
	}
	return tctx.stepIDeleteZookeeperNode(dcs.JoinPath("/test", dcs.PathHANodesPrefix, host))
}

func (tctx *testContext) stepValkeyOnHostKilled(host string) error {
	cmd := "supervisorctl signal KILL valkey"
	_, _, err := tctx.composer.RunCommand(host, cmd, 10*time.Second)
	return err
}

func (tctx *testContext) stepValkeyOnHostStarted(host string) error {
	cmd := "supervisorctl start valkey"
	_, _, err := tctx.composer.RunCommand(host, cmd, 10*time.Second)
	return err
}

func (tctx *testContext) stepValkeyOnHostRestarted(host string) error {
	cmd := "supervisorctl restart valkey"
	_, _, err := tctx.composer.RunCommand(host, cmd, 30*time.Second)
	return err
}

func (tctx *testContext) stepValkeyOnHostStopped(host string) error {
	cmd := "supervisorctl signal TERM valkey"
	_, _, err := tctx.composer.RunCommand(host, cmd, 10*time.Second)
	return err
}

func (tctx *testContext) stepHostShouldHaveFile(node string, path string) error {
	remoteFile, err := tctx.composer.GetFile(node, path)
	if err != nil {
		return err
	}
	err = remoteFile.Close()
	if err != nil {
		return err
	}
	return nil
}

func (tctx *testContext) stepHostShouldHaveFileWithin(node string, path string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepHostShouldHaveFile(node, path)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepPathExists(path, host string) error {
	cmd := fmt.Sprintf("stat %s", path)
	retCode, _, err := tctx.composer.RunCommand(host, cmd, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to check path %s on %s: %s", path, host, err)
	}
	if retCode != 0 {
		return fmt.Errorf("expected %s to exist on %s but it's not", path, host)
	}
	return nil
}

func (tctx *testContext) stepPathDoesNotExist(path, host string) error {
	cmd := fmt.Sprintf("stat %s", path)
	retCode, _, err := tctx.composer.RunCommand(host, cmd, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to check path %s on %s: %s", path, host, err)
	}
	if retCode == 0 {
		return fmt.Errorf("expected %s to be absent on %s but it exists", path, host)
	}
	return nil
}

func (tctx *testContext) stepIRunCommandOnHost(host string, body *godog.DocString) error {
	cmd := strings.TrimSpace(body.Content)
	var err error
	tctx.commandRetcode, tctx.commandOutput, err = tctx.composer.RunCommand(host, cmd, 10*time.Second)
	return err
}

func (tctx *testContext) stepIRunAsyncCommandOnHost(host string, body *godog.DocString) error {
	cmd := strings.TrimSpace(body.Content)
	return tctx.composer.RunAsyncCommand(host, cmd)
}

func (tctx *testContext) stepIRunCommandOnHostWithTimeout(host string, timeout int, body *godog.DocString) error {
	cmd := strings.TrimSpace(body.Content)
	var err error
	tctx.commandRetcode, tctx.commandOutput, err = tctx.composer.RunCommand(host, cmd, time.Duration(timeout)*time.Second)
	return err
}

func (tctx *testContext) stepIRunCommandOnHostUntilResultMatch(host string, pattern string, timeout int, body *godog.DocString) error {
	matcher, err := matchers.GetMatcher("regexp")
	if err != nil {
		return err
	}

	var lastError error
	testutil.Retry(func() bool {
		cmd := strings.TrimSpace(body.Content)
		tctx.commandRetcode, tctx.commandOutput, lastError = tctx.composer.RunCommand(host, cmd, time.Duration(timeout)*time.Second)
		if lastError != nil {
			return false
		}
		lastError = matcher(tctx.commandOutput, strings.TrimSpace(pattern))
		return lastError == nil
	}, time.Duration(timeout)*time.Second, time.Second)

	return lastError
}

func (tctx *testContext) stepCommandReturnCodeShouldBe(code int) error {
	if tctx.commandRetcode != code {
		return fmt.Errorf("command return code is %d, while expected %d\n%s", tctx.commandRetcode, code, tctx.commandOutput)
	}
	return nil
}

func (tctx *testContext) stepCommandOutputShouldMatch(matcher string, body *godog.DocString) error {
	m, err := matchers.GetMatcher(matcher)
	if err != nil {
		return err
	}
	return m(tctx.commandOutput, strings.TrimSpace(body.Content))
}

func (tctx *testContext) stepIRunCmdOnHost(host string, body *godog.DocString) error {
	split := strings.Split(strings.TrimSpace(body.Content), "\"")
	var args []string
	for index, arg := range split {
		if index%2 == 1 {
			args = append(args, strings.TrimSpace(arg))
		} else {
			args = append(args, strings.Split(strings.TrimSpace(arg), " ")...)
		}
	}
	_, err := tctx.runValkeyCmd(host, args)
	return err
}

func (tctx *testContext) stepValkeyCmdResultShouldMatch(matcher string, body *godog.DocString) error {
	m, err := matchers.GetMatcher(matcher)
	if err != nil {
		return err
	}
	return m(tctx.valkeyCmdResult, strings.TrimSpace(body.Content))
}

func (tctx *testContext) stepIRunSenticacheCmdOnHost(host string, body *godog.DocString) error {
	split := strings.Split(strings.TrimSpace(body.Content), "\"")
	var args []string
	for index, arg := range split {
		if index%2 == 1 {
			args = append(args, strings.TrimSpace(arg))
		} else {
			args = append(args, strings.Split(strings.TrimSpace(arg), " ")...)
		}
	}
	_, err := tctx.runSenticacheCmd(host, args)
	return err
}

func (tctx *testContext) stepSenticacheCmdResultShouldMatch(matcher string, body *godog.DocString) error {
	m, err := matchers.GetMatcher(matcher)
	if err != nil {
		return err
	}
	return m(tctx.senticacheCmdResult, strings.TrimSpace(body.Content))
}

func (tctx *testContext) stepBreakReplicationOnHost(host string) error {
	if _, err := tctx.runValkeyCmd(host, []string{"CONFIG", "SET", "repl-paused", "yes"}); err != nil {
		return err
	}
	return nil
}

func (tctx *testContext) stepIGetZookeeperNode(node string) error {
	data, _, err := tctx.zk.Get(node)
	if err != nil {
		return err
	}
	tctx.zkQueryResult = string(data)
	return nil
}

func (tctx *testContext) createZookeeperNode(node string) error {
	if ok, _, err := tctx.zk.Exists(node); err == nil && ok {
		return nil
	}
	data, err := tctx.zk.Create(node, []byte{}, 0, tctx.acl)
	if err != nil {
		return err
	}
	tctx.zkQueryResult = data
	return nil
}

func (tctx *testContext) stepISetZookeeperNode(node string, body *godog.DocString) error {
	data := []byte(strings.TrimSpace(body.Content))
	if !json.Valid(data) {
		return fmt.Errorf("node value is not valid json")
	}
	_, stat, err := tctx.zk.Get(node)
	if err != nil && err != zk.ErrNoNode {
		return err
	}
	if err == zk.ErrNoNode {
		_, err = tctx.zk.Create(node, data, 0, tctx.acl)
	} else {
		_, err = tctx.zk.Set(node, data, stat.Version)
	}
	return err
}

func (tctx *testContext) stepIDeleteZookeeperNode(node string) error {
	_, stat, err := tctx.zk.Get(node)
	if err != nil {
		return err
	}
	return tctx.zk.Delete(node, stat.Version)
}

func (tctx *testContext) stepZookeeperNodeShouldMatch(node, matcher string, body *godog.DocString) error {
	err := tctx.stepIGetZookeeperNode(node)
	if err != nil {
		return err
	}
	m, err := matchers.GetMatcher(matcher)
	if err != nil {
		return err
	}
	return m(tctx.zkQueryResult, strings.TrimSpace(body.Content))
}

func (tctx *testContext) stepZookeeperNodeShouldMatchWithin(node, matcher string, timeout int, body *godog.DocString) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepZookeeperNodeShouldMatch(node, matcher, body)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepZookeeperNodeShouldExist(node string) error {
	err := tctx.stepIGetZookeeperNode(node)
	if err != nil {
		return err
	}
	return nil
}

func (tctx *testContext) stepZookeeperNodeShouldExistWithin(node string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepZookeeperNodeShouldExist(node)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepZookeeperNodeShouldNotExist(node string) error {
	err := tctx.stepIGetZookeeperNode(node)
	if err == zk.ErrNoNode {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("zookeeper node %s exists, but it should not", node)
}

func (tctx *testContext) stepZookeeperNodeShouldNotExistWithin(node string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepZookeeperNodeShouldNotExist(node)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepValkeyHostShouldBeMaster(host string) error {
	res, err := tctx.runValkeyCmd(host, []string{"ROLE"})
	if err != nil {
		return err
	}
	m := matchers.RegexpMatcher
	return m(res, ".*master.*")
}

func (tctx *testContext) stepValkeyHostShouldBeReplicaOf(host, master string) error {
	res, err := tctx.runValkeyCmd(host, []string{"INFO", "replication"})
	if err != nil {
		return err
	}
	m := matchers.RegexpMatcher
	ip, err := tctx.composer.GetIP(master)
	if err != nil {
		return err
	}
	return m(res, fmt.Sprintf(".*master_host:(%s|%s).*", master, ip))
}

func (tctx *testContext) stepValkeyHostShouldBecomeReplicaOfWithin(host, master string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepValkeyHostShouldBeReplicaOf(host, master)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepReplicationOnValkeyHostShouldRunFine(host string) error {
	res, err := tctx.runValkeyCmd(host, []string{"INFO", "replication"})
	if err != nil {
		return err
	}
	m := matchers.RegexpMatcher
	return m(res, ".*master_link_status:up.*")
}

func (tctx *testContext) stepReplicationOnValkeyHostShouldRunFineWithin(host string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepReplicationOnValkeyHostShouldRunFine(host)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepValkeyHostShouldBecomeUnavailableWithin(host string, timeout int) error {
	addr, err := tctx.composer.GetAddr(host, valkeyPort)
	if err != nil {
		return fmt.Errorf("failed to get valkey addr %s: %s", host, err)
	}
	testutil.Retry(func() bool {
		var conn client.Client
		conn, err = tctx.connectValkey(addr, time.Second)
		if err == nil {
			conn.Close()
			return false
		}
		return true
	}, time.Duration(timeout*int(time.Second)), time.Second)
	if err == nil {
		return fmt.Errorf("valkey host %s is still available", host)
	}
	return nil
}

func (tctx *testContext) stepValkeyHostShouldBecomeAvailableWithin(host string, timeout int) error {
	addr, err := tctx.composer.GetAddr(host, valkeyPort)
	if err != nil {
		return fmt.Errorf("failed to get valkey addr %s: %s", host, err)
	}
	testutil.Retry(func() bool {
		var conn client.Client
		conn, err = tctx.connectValkey(addr, valkeyConnectTimeout)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepSenticacheHostShouldHaveMaster(host, master string) error {
	res, err := tctx.runSenticacheCmd(host, []string{"INFO"})
	if err != nil {
		return err
	}
	m := matchers.RegexpMatcher
	ip, err := tctx.composer.GetIP(master)
	if err != nil {
		return err
	}
	return m(res, fmt.Sprintf(".*master0:name=functest,status=ok,address=(%s|%s).*", master, ip))
}

func (tctx *testContext) stepSenticacheHostShouldHaveMasterWithin(host, master string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepSenticacheHostShouldHaveMaster(host, master)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepISaveZookeeperQueryResultAs(varname string) error {
	var j interface{}
	if tctx.zkQueryResult != "" {
		if err := json.Unmarshal([]byte(tctx.zkQueryResult), &j); err != nil {
			return err
		}
	}
	tctx.variables[varname] = j
	return nil
}

func (tctx *testContext) stepISaveValkeyCmdResultAs(varname string) error {
	tctx.variables[varname] = tctx.valkeyCmdResult
	return nil
}

func (tctx *testContext) stepISaveCommandOutputAs(varname string) error {
	tctx.variables[varname] = strings.TrimSpace(tctx.commandOutput)
	return nil
}

func (tctx *testContext) stepISaveValAs(val, varname string) error {
	tctx.variables[varname] = val
	return nil
}

func (tctx *testContext) stepIWaitFor(timeout int) error {
	time.Sleep(time.Duration(timeout) * time.Second)
	return nil
}

func (tctx *testContext) stepInfoFileOnHostMatch(filepath, host, matcher string, body *godog.DocString) error {
	m, err := matchers.GetMatcher(matcher)
	if err != nil {
		return err
	}

	testutil.Retry(func() bool {
		remoteFile, err := tctx.composer.GetFile(host, filepath)
		if err != nil {
			return true
		}
		content, err := io.ReadAll(remoteFile)
		if err != nil {
			return true
		}
		err = remoteFile.Close()
		if err != nil {
			return true
		}
		if err = m(string(content), strings.TrimSpace(body.Content)); err != nil {
			return true
		}
		return false
	}, time.Second*10, time.Second)

	return err
}

func InitializeScenario(s *godog.ScenarioContext) {
	tctx, err := newTestContext()
	if err != nil {
		panic(err)
	}

	s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		tctx.cleanup()
		return ctx, nil
	})
	s.StepContext().Before(func(ctx context.Context, step *godog.Step) (context.Context, error) {
		tctx.templateErr = tctx.templateStep(step)
		return ctx, nil
	})
	s.StepContext().After(func(ctx context.Context, step *godog.Step, _ godog.StepResultStatus, _ error) (context.Context, error) {
		if tctx.templateErr != nil {
			log.Fatalf("Error in templating %s: %v\n", step.Text, tctx.templateErr)
		}
		return ctx, nil
	})
	s.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if err != nil {
			name := scenario.Name
			name = strings.ReplaceAll(name, " ", "_")
			err2 := tctx.saveLogs(name)
			if err2 != nil {
				log.Printf("failed to save logs: %v", err2)
			}
			if v, _ := os.LookupEnv("GODOG_NO_CLEANUP"); v != "" {
				return ctx, nil
			}
		}
		tctx.cleanup()
		return ctx, nil
	})

	// host manipulation
	s.Step(`^clustered shard is up and running$`, tctx.stepClusteredShardIsUpAndRunning)
	s.Step(`^sentinel shard is up and running$`, tctx.stepSentinelShardIsUpAndRunning)
	s.Step(`^persistence is disabled$`, tctx.stepPersistenceDisabled)
	s.Step(`^host "([^"]*)" is stopped$`, tctx.stepHostIsStopped)
	s.Step(`^host "([^"]*)" is detached from the network$`, tctx.stepHostIsDetachedFromTheNetwork)
	s.Step(`^host "([^"]*)" is started$`, tctx.stepHostIsStarted)
	s.Step(`^host "([^"]*)" is attached to the network$`, tctx.stepHostIsAttachedToTheNetwork)
	s.Step(`^port "(\d+)" on host "([^"]*)" is blocked$`, tctx.stepPortOnHostIsBlocked)
	s.Step(`^port "(\d+)" on host "([^"]*)" is unblocked$`, tctx.stepPortOnHostIsUnBlocked)
	s.Step(`^host "([^"]*)" is added`, tctx.stepHostIsAdded)
	s.Step(`^host "([^"]*)" is deleted$`, tctx.stepHostIsDeleted)

	// host checking
	s.Step(`^host "([^"]*)" should have file "([^"]*)"$`, tctx.stepHostShouldHaveFile)
	s.Step(`^host "([^"]*)" should have file "([^"]*)" within "(\d+)" seconds$`, tctx.stepHostShouldHaveFileWithin)

	// path checking
	s.Step(`^path "([^"]*)" does not exist on "([^"]*)"$`, tctx.stepPathDoesNotExist)
	s.Step(`^path "([^"]*)" exists on "([^"]*)"$`, tctx.stepPathExists)

	// command execution
	s.Step(`^I run command on host "([^"]*)"$`, tctx.stepIRunCommandOnHost)
	s.Step(`^I run command on host "([^"]*)" with timeout "(\d+)" seconds$`, tctx.stepIRunCommandOnHostWithTimeout)
	s.Step(`^I run async command on host "([^"]*)"$`, tctx.stepIRunAsyncCommandOnHost)
	s.Step(`^I run command on host "([^"]*)" until result match regexp "([^"]*)" with timeout "(\d+)" seconds$`, tctx.stepIRunCommandOnHostUntilResultMatch)
	s.Step(`^command return code should be "(\d+)"$`, tctx.stepCommandReturnCodeShouldBe)
	s.Step(`^command output should match (\w+)$`, tctx.stepCommandOutputShouldMatch)
	s.Step(`^I run command on valkey host "([^"]*)"$`, tctx.stepIRunCmdOnHost)
	s.Step(`^valkey cmd result should match (\w+)$`, tctx.stepValkeyCmdResultShouldMatch)
	s.Step(`^I run command on senticache host "([^"]*)"$`, tctx.stepIRunSenticacheCmdOnHost)
	s.Step(`^senticache cmd result should match (\w+)$`, tctx.stepSenticacheCmdResultShouldMatch)

	// zookeeper manipulation
	s.Step(`^I get zookeeper node "([^"]*)"$`, tctx.stepIGetZookeeperNode)
	s.Step(`^I set zookeeper node "([^"]*)" to$`, tctx.stepISetZookeeperNode)
	s.Step(`^I delete zookeeper node "([^"]*)"$`, tctx.stepIDeleteZookeeperNode)

	// zookeeper checking
	s.Step(`^zookeeper node "([^"]*)" should match (\w+)$`, tctx.stepZookeeperNodeShouldMatch)
	s.Step(`^zookeeper node "([^"]*)" should match (\w+) within "(\d+)" seconds$`, tctx.stepZookeeperNodeShouldMatchWithin)
	s.Step(`^zookeeper node "([^"]*)" should exist$`, tctx.stepZookeeperNodeShouldExist)
	s.Step(`^zookeeper node "([^"]*)" should exist within "(\d+)" seconds$`, tctx.stepZookeeperNodeShouldExistWithin)
	s.Step(`^zookeeper node "([^"]*)" should not exist$`, tctx.stepZookeeperNodeShouldNotExist)
	s.Step(`^zookeeper node "([^"]*)" should not exist within "(\d+)" seconds$`, tctx.stepZookeeperNodeShouldNotExistWithin)

	// valkey checking
	s.Step(`^valkey host "([^"]*)" should be master$`, tctx.stepValkeyHostShouldBeMaster)
	s.Step(`^valkey host "([^"]*)" should be replica of "([^"]*)"$`, tctx.stepValkeyHostShouldBeReplicaOf)
	s.Step(`^valkey host "([^"]*)" should become replica of "([^"]*)" within "(\d+)" seconds$`, tctx.stepValkeyHostShouldBecomeReplicaOfWithin)
	s.Step(`^replication on valkey host "([^"]*)" should run fine$`, tctx.stepReplicationOnValkeyHostShouldRunFine)
	s.Step(`^replication on valkey host "([^"]*)" should run fine within "(\d+)" seconds$`, tctx.stepReplicationOnValkeyHostShouldRunFineWithin)

	s.Step(`^valkey host "([^"]*)" should become unavailable within "(\d+)" seconds$`, tctx.stepValkeyHostShouldBecomeUnavailableWithin)
	s.Step(`^valkey host "([^"]*)" should become available within "(\d+)" seconds$`, tctx.stepValkeyHostShouldBecomeAvailableWithin)

	// senticache checking
	s.Step(`^senticache host "([^"]*)" should have master "([^"]*)"$`, tctx.stepSenticacheHostShouldHaveMaster)
	s.Step(`^senticache host "([^"]*)" should have master "([^"]*)" within "(\d+)" seconds$`, tctx.stepSenticacheHostShouldHaveMasterWithin)

	// valkey manipulation
	s.Step(`^valkey on host "([^"]*)" is killed$`, tctx.stepValkeyOnHostKilled)
	s.Step(`^valkey on host "([^"]*)" is started$`, tctx.stepValkeyOnHostStarted)
	s.Step(`^valkey on host "([^"]*)" is restarted$`, tctx.stepValkeyOnHostRestarted)
	s.Step(`^valkey on host "([^"]*)" is stopped$`, tctx.stepValkeyOnHostStopped)
	s.Step(`^I break replication on host "([^"]*)"$`, tctx.stepBreakReplicationOnHost)

	// variables
	s.Step(`^I save zookeeper query result as "([^"]*)"$`, tctx.stepISaveZookeeperQueryResultAs)
	s.Step(`^I save command output as "([^"]*)"$`, tctx.stepISaveCommandOutputAs)
	s.Step(`^I save valkey cmd result as "([^"]*)"$`, tctx.stepISaveValkeyCmdResultAs)
	s.Step(`^I save "([^"]*)" as "([^"]*)"$`, tctx.stepISaveValAs)

	// misc
	s.Step(`^I wait for "(\d+)" seconds$`, tctx.stepIWaitFor)
	s.Step(`^info file "([^"]*)" on "([^"]*)" match (\w+)$`, tctx.stepInfoFileOnHostMatch)
}

func TestRdsync(t *testing.T) {
	features := "features"
	if featureEnv, ok := os.LookupEnv("GODOG_FEATURE"); ok {
		features = fmt.Sprintf("features/%s.feature", featureEnv)
	}
	stopOnFailure := true
	if _, ok := os.LookupEnv("GODOG_NO_STOP_ON_FAILURE"); ok {
		stopOnFailure = false
	}
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:        "pretty",
			Paths:         []string{features},
			Strict:        true,
			StopOnFailure: stopOnFailure,
			Concurrency:   1,
		},
	}
	if suite.Run() != 0 {
		t.Fail()
	}
}
