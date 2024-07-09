package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/go-zookeeper/zk"
	"github.com/redis/go-redis/v9"

	"github.com/yandex/rdsync/internal/dcs"
	"github.com/yandex/rdsync/tests/testutil"
	"github.com/yandex/rdsync/tests/testutil/matchers"
)

const (
	zkName                     = "zoo"
	zkPort                     = 2181
	zkConnectTimeout           = 5 * time.Second
	redisName                  = "redis"
	redisPort                  = 6379
	senticachePort             = 26379
	redisPassword              = "functestpassword"
	redisConnectTimeout        = 30 * time.Second
	redisInitialConnectTimeout = 2 * time.Minute
	redisCmdTimeout            = 15 * time.Second
	testUser                   = "testuser"
	testPassword               = "testpassword123"
)

var redisLogsToSave = map[string]string{
	"/var/log/supervisor.log":       "supervisor.log",
	"/var/log/rdsync.log":           "rdsync.log",
	"/var/log/redis/server.log":     "redis.log",
	"/var/log/redis/senticache.log": "senticache.log",
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
	conns               map[string]*redis.Client
	senticaches         map[string]*redis.Client
	zkQueryResult       string
	redisCmdResult      string
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
	tctx.conns = make(map[string]*redis.Client)
	tctx.senticaches = make(map[string]*redis.Client)
	tctx.acl = zk.DigestACL(zk.PermAll, testUser, testPassword)
	return tctx, nil
}

func (tctx *testContext) saveLogs(scenario string) error {
	for _, service := range tctx.composer.Services() {
		var logsToSave map[string]string
		switch {
		case strings.HasPrefix(service, redisName):
			logsToSave = redisLogsToSave
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
		if err := conn.Close(); err != nil {
			log.Printf("failed to close redis connection: %s", err)
		}
	}
	tctx.conns = make(map[string]*redis.Client)
	for _, conn := range tctx.senticaches {
		if err := conn.Close(); err != nil {
			log.Printf("failed to close senticache connection: %s", err)
		}
	}
	tctx.senticaches = make(map[string]*redis.Client)
	if err := tctx.composer.Down(); err != nil {
		log.Printf("failed to tear down compose: %s", err)
	}

	tctx.variables = make(map[string]interface{})
	tctx.composerEnv = make([]string, 0)
	tctx.zkQueryResult = ""
	tctx.redisCmdResult = ""
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

func (tctx *testContext) connectRedis(addr string, timeout time.Duration) (*redis.Client, error) {
	opts := redis.Options{
		Addr:         addr,
		Password:     redisPassword,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		PoolSize:     1,
		MinIdleConns: 1,
		Protocol:     2,
	}
	conn := redis.NewClient(&opts)
	// redis connection is lazy, so we need ping it
	var err error
	testutil.Retry(func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), redisCmdTimeout)
		defer cancel()
		err = conn.Ping(ctx).Err()
		return err == nil
	}, timeout, time.Second)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (tctx *testContext) connectSenticache(addr string, timeout time.Duration) (*redis.Client, error) {
	opts := redis.Options{
		Addr:         addr,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		PoolSize:     1,
		MinIdleConns: 1,
	}
	conn := redis.NewClient(&opts)
	// redis connection is lazy, so we need ping it
	var err error
	testutil.Retry(func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), redisCmdTimeout)
		defer cancel()
		err = conn.Ping(ctx).Err()
		return err == nil
	}, timeout, time.Second)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (tctx *testContext) getRedisConnection(host string) (*redis.Client, error) {
	conn, ok := tctx.conns[host]
	if !ok {
		return nil, fmt.Errorf("redis %s is not in our host list", host)
	}
	err := conn.Ping(context.Background()).Err()
	if err == nil {
		return conn, nil
	}
	addr, err := tctx.composer.GetAddr(host, redisPort)
	if err != nil {
		return nil, fmt.Errorf("failed to get redis addr %s: %s", host, err)
	}
	conn, err = tctx.connectRedis(addr, redisConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redis %s: %s", host, err)
	}
	tctx.conns[host] = conn
	return conn, nil
}

func (tctx *testContext) getSenticacheConnection(host string) (*redis.Client, error) {
	conn, ok := tctx.senticaches[host]
	if !ok {
		return nil, fmt.Errorf("senticache %s is not in our host list", host)
	}
	err := conn.Ping(context.Background()).Err()
	if err == nil {
		return conn, nil
	}
	addr, err := tctx.composer.GetAddr(host, senticachePort)
	if err != nil {
		return nil, fmt.Errorf("failed to get senticache addr %s: %s", host, err)
	}
	conn, err = tctx.connectSenticache(addr, redisConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to senticache %s: %s", host, err)
	}
	tctx.senticaches[host] = conn
	return conn, nil
}

func (tctx *testContext) runRedisCmd(host string, cmd []string) (string, error) {
	conn, err := tctx.getRedisConnection(host)
	if err != nil {
		return "", err
	}

	tctx.redisCmdResult = ""
	ctx, cancel := context.WithTimeout(context.Background(), redisCmdTimeout)
	defer cancel()
	var iargs []interface{}
	for _, arg := range cmd {
		iargs = append(iargs, arg)
	}
	result := conn.Do(ctx, iargs...)

	err = result.Err()
	if err != nil {
		tctx.redisCmdResult = err.Error()
	} else {
		tctx.redisCmdResult = result.String()
	}

	return tctx.redisCmdResult, err
}

func (tctx *testContext) runSenticacheCmd(host string, cmd []string) (string, error) {
	conn, err := tctx.getSenticacheConnection(host)
	if err != nil {
		return "", err
	}

	tctx.senticacheCmdResult = ""
	ctx, cancel := context.WithTimeout(context.Background(), redisCmdTimeout)
	defer cancel()
	var iargs []interface{}
	for _, arg := range cmd {
		iargs = append(iargs, arg)
	}
	result := conn.Do(ctx, iargs...)

	err = result.Err()
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
		"redis",
		time.Minute)
	if err != nil {
		return fmt.Errorf("failed to generate certs in redis hosts: %s", err)
	}

	if err = tctx.createZookeeperNode("/test"); err != nil {
		return fmt.Errorf("failed to create namespace zk node due %s", err)
	}
	if err = tctx.createZookeeperNode(dcs.JoinPath("/test", dcs.PathHANodesPrefix)); err != nil {
		return fmt.Errorf("failed to create path prefix zk node due %s", err)
	}

	// prepare redis nodes
	for _, service := range tctx.composer.Services() {
		if strings.HasPrefix(service, redisName) {
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
	_, _, err = tctx.composer.RunCommand("redis1", "setup_cluster.sh", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("redis2", "setup_cluster.sh redis1", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("redis3", "setup_cluster.sh redis1", 1*time.Minute)
	if err != nil {
		return err
	}

	// check redis nodes
	for _, service := range tctx.composer.Services() {
		if strings.HasPrefix(service, redisName) {
			addr, err := tctx.composer.GetAddr(service, redisPort)
			if err != nil {
				return fmt.Errorf("failed to get redis addr %s: %s", service, err)
			}
			conn, err := tctx.connectRedis(addr, redisInitialConnectTimeout)
			if err != nil {
				return fmt.Errorf("failed to connect to redis %s: %s", service, err)
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
	_, _, err = tctx.composer.RunCommand("redis1", "setup_sentinel.sh", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("redis2", "setup_sentinel.sh redis1", 1*time.Minute)
	if err != nil {
		return err
	}
	_, _, err = tctx.composer.RunCommand("redis3", "setup_sentinel.sh redis1", 1*time.Minute)
	if err != nil {
		return err
	}
	// check redis nodes
	for _, service := range tctx.composer.Services() {
		if strings.HasPrefix(service, redisName) {
			addr, err := tctx.composer.GetAddr(service, redisPort)
			if err != nil {
				return fmt.Errorf("failed to get redis addr %s: %s", service, err)
			}
			conn, err := tctx.connectRedis(addr, redisInitialConnectTimeout)
			if err != nil {
				return fmt.Errorf("failed to connect to redis %s: %s", service, err)
			}
			tctx.conns[service] = conn
			saddr, err2 := tctx.composer.GetAddr(service, senticachePort)
			if err2 != nil {
				return fmt.Errorf("failed to get senticache addr %s: %s", service, err2)
			}
			sconn, err2 := tctx.connectSenticache(saddr, redisInitialConnectTimeout)
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
		if strings.HasPrefix(service, redisName) {
			_, err := tctx.runRedisCmd(service, []string{"CONFIG", "SET", "appendonly", "no"})
			if err != nil {
				return err
			}
			_, err = tctx.runRedisCmd(service, []string{"CONFIG", "SET", "save", ""})
			if err != nil {
				return err
			}
			_, _, err = tctx.composer.RunCommand(service, "echo 'appendonly no' >> /etc/redis/redis.conf", 10*time.Second)
			if err != nil {
				return err
			}
			_, _, err = tctx.composer.RunCommand(service, "echo 'save \\'\\'' >> /etc/redis/redis.conf", 10*time.Second)
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

func (tctx *testContext) stepRedisOnHostKilled(host string) error {
	cmd := "supervisorctl signal KILL redis"
	_, _, err := tctx.composer.RunCommand(host, cmd, 10*time.Second)
	return err
}

func (tctx *testContext) stepRedisOnHostStarted(host string) error {
	cmd := "supervisorctl start redis"
	_, _, err := tctx.composer.RunCommand(host, cmd, 10*time.Second)
	return err
}

func (tctx *testContext) stepRedisOnHostRestarted(host string) error {
	cmd := "supervisorctl restart redis"
	_, _, err := tctx.composer.RunCommand(host, cmd, 30*time.Second)
	return err
}

func (tctx *testContext) stepRedisOnHostStopped(host string) error {
	cmd := "supervisorctl signal TERM redis"
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
	splitted := strings.Split(strings.TrimSpace(body.Content), "\"")
	var args []string
	for index, arg := range splitted {
		if index%2 == 1 {
			args = append(args, strings.TrimSpace(arg))
		} else {
			args = append(args, strings.Split(strings.TrimSpace(arg), " ")...)
		}
	}
	_, err := tctx.runRedisCmd(host, args)
	return err
}

func (tctx *testContext) stepRedisCmdResultShouldMatch(matcher string, body *godog.DocString) error {
	m, err := matchers.GetMatcher(matcher)
	if err != nil {
		return err
	}
	return m(tctx.redisCmdResult, strings.TrimSpace(body.Content))
}

func (tctx *testContext) stepIRunSenticacheCmdOnHost(host string, body *godog.DocString) error {
	splitted := strings.Split(strings.TrimSpace(body.Content), "\"")
	var args []string
	for index, arg := range splitted {
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
	if _, err := tctx.runRedisCmd(host, []string{"CONFIG", "SET", "repl-paused", "yes"}); err != nil {
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

func (tctx *testContext) stepRedisHostShouldBeMaster(host string) error {
	res, err := tctx.runRedisCmd(host, []string{"ROLE"})
	if err != nil {
		return err
	}
	m := matchers.RegexpMatcher
	return m(res, ".*master.*")
}

func (tctx *testContext) stepRedisHostShouldBeReplicaOf(host, master string) error {
	res, err := tctx.runRedisCmd(host, []string{"INFO", "replication"})
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

func (tctx *testContext) stepRedisHostShouldBecomeReplicaOfWithin(host, master string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepRedisHostShouldBeReplicaOf(host, master)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepReplicationOnRedisHostShouldRunFine(host string) error {
	res, err := tctx.runRedisCmd(host, []string{"INFO", "replication"})
	if err != nil {
		return err
	}
	m := matchers.RegexpMatcher
	return m(res, ".*master_link_status:up.*")
}

func (tctx *testContext) stepReplicationOnRedisHostShouldRunFineWithin(host string, timeout int) error {
	var err error
	testutil.Retry(func() bool {
		err = tctx.stepReplicationOnRedisHostShouldRunFine(host)
		return err == nil
	}, time.Duration(timeout*int(time.Second)), time.Second)
	return err
}

func (tctx *testContext) stepRedisHostShouldBecomeUnavailableWithin(host string, timeout int) error {
	addr, err := tctx.composer.GetAddr(host, redisPort)
	if err != nil {
		return fmt.Errorf("failed to get redis addr %s: %s", host, err)
	}
	testutil.Retry(func() bool {
		var conn *redis.Client
		conn, err = tctx.connectRedis(addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return false
		}
		return true
	}, time.Duration(timeout*int(time.Second)), time.Second)
	if err == nil {
		return fmt.Errorf("redis host %s is still available", host)
	}
	return nil
}

func (tctx *testContext) stepRedisHostShouldBecomeAvailableWithin(host string, timeout int) error {
	addr, err := tctx.composer.GetAddr(host, redisPort)
	if err != nil {
		return fmt.Errorf("failed to get redis addr %s: %s", host, err)
	}
	testutil.Retry(func() bool {
		var conn *redis.Client
		conn, err = tctx.connectRedis(addr, redisConnectTimeout)
		if err == nil {
			_ = conn.Close()
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

func (tctx *testContext) stepISaveZookeperQueryResultAs(varname string) error {
	var j interface{}
	if tctx.zkQueryResult != "" {
		if err := json.Unmarshal([]byte(tctx.zkQueryResult), &j); err != nil {
			return err
		}
	}
	tctx.variables[varname] = j
	return nil
}

func (tctx *testContext) stepISaveRedisCmdResultAs(varname string) error {
	tctx.variables[varname] = tctx.redisCmdResult
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
			name = strings.Replace(name, " ", "_", -1)
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

	// command execution
	s.Step(`^I run command on host "([^"]*)"$`, tctx.stepIRunCommandOnHost)
	s.Step(`^I run command on host "([^"]*)" with timeout "(\d+)" seconds$`, tctx.stepIRunCommandOnHostWithTimeout)
	s.Step(`^I run async command on host "([^"]*)"$`, tctx.stepIRunAsyncCommandOnHost)
	s.Step(`^I run command on host "([^"]*)" until result match regexp "([^"]*)" with timeout "(\d+)" seconds$`, tctx.stepIRunCommandOnHostUntilResultMatch)
	s.Step(`^command return code should be "(\d+)"$`, tctx.stepCommandReturnCodeShouldBe)
	s.Step(`^command output should match (\w+)$`, tctx.stepCommandOutputShouldMatch)
	s.Step(`^I run command on redis host "([^"]*)"$`, tctx.stepIRunCmdOnHost)
	s.Step(`^redis cmd result should match (\w+)$`, tctx.stepRedisCmdResultShouldMatch)
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

	// redis checking
	s.Step(`^redis host "([^"]*)" should be master$`, tctx.stepRedisHostShouldBeMaster)
	s.Step(`^redis host "([^"]*)" should be replica of "([^"]*)"$`, tctx.stepRedisHostShouldBeReplicaOf)
	s.Step(`^redis host "([^"]*)" should become replica of "([^"]*)" within "(\d+)" seconds$`, tctx.stepRedisHostShouldBecomeReplicaOfWithin)
	s.Step(`^replication on redis host "([^"]*)" should run fine$`, tctx.stepReplicationOnRedisHostShouldRunFine)
	s.Step(`^replication on redis host "([^"]*)" should run fine within "(\d+)" seconds$`, tctx.stepReplicationOnRedisHostShouldRunFineWithin)

	s.Step(`^redis host "([^"]*)" should become unavailable within "(\d+)" seconds$`, tctx.stepRedisHostShouldBecomeUnavailableWithin)
	s.Step(`^redis host "([^"]*)" should become available within "(\d+)" seconds$`, tctx.stepRedisHostShouldBecomeAvailableWithin)

	// senticache checking
	s.Step(`^senticache host "([^"]*)" should have master "([^"]*)"$`, tctx.stepSenticacheHostShouldHaveMaster)
	s.Step(`^senticache host "([^"]*)" should have master "([^"]*)" within "(\d+)" seconds$`, tctx.stepSenticacheHostShouldHaveMasterWithin)

	// redis manipulation
	s.Step(`^redis on host "([^"]*)" is killed$`, tctx.stepRedisOnHostKilled)
	s.Step(`^redis on host "([^"]*)" is started$`, tctx.stepRedisOnHostStarted)
	s.Step(`^redis on host "([^"]*)" is restarted$`, tctx.stepRedisOnHostRestarted)
	s.Step(`^redis on host "([^"]*)" is stopped$`, tctx.stepRedisOnHostStopped)
	s.Step(`^I break replication on host "([^"]*)"$`, tctx.stepBreakReplicationOnHost)

	// variables
	s.Step(`^I save zookeeper query result as "([^"]*)"$`, tctx.stepISaveZookeperQueryResultAs)
	s.Step(`^I save command output as "([^"]*)"$`, tctx.stepISaveCommandOutputAs)
	s.Step(`^I save redis cmd result as "([^"]*)"$`, tctx.stepISaveRedisCmdResultAs)
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
