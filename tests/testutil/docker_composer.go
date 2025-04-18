package testutil

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const defaultDockerTimeout = 30 * time.Second
const defaultDockerComposeTimeout = 90 * time.Second
const shell = "/bin/bash"

// Composer manipulate images/vm's during integration tests
type Composer interface {
	// Brings all containers/VMs up according to config
	Up(env []string) error
	// Trears all containers/VMs dowwn
	Down() error
	// Returns names/ids of running containers
	Services() []string
	// Returns real exposed addr (ip:port) for given service/port
	GetAddr(service string, port int) (string, error)
	// Returns internal ip address of given service
	GetIP(service string) (string, error)
	// Stops container/VM
	Stop(service string) error
	// Starts container/VM
	Start(service string) error
	// Detaches container/VM from network
	DetachFromNet(service string) error
	// Attaches container/VM to network
	AttachToNet(service string) error
	// Blocks port on host
	BlockPort(service string, port int) error
	// Blocks host/port pair on host
	BlockHostPort(service string, host string, port int) error
	// Blocks incoming connections from host
	BlockHostConnections(service string, host string) error
	// Unblocks port on host
	UnBlockPort(service string, port int) error
	// Unblocks host/port pair on host
	UnBlockHostPort(service string, host string, port int) error
	// Unblocks incoming connections from host
	UnBlockHostConnections(service string, host string) error
	// Executes command inside container/VM with given timeout.
	// Returns command retcode and output (stdoud and stderr are mixed)
	RunCommand(service, cmd string, timeout time.Duration) (retcode int, output string, err error)
	RunCommandAtHosts(cmd, hostsSubstring string, timeout time.Duration) error
	// Executes command inside container/VM with given timeout.
	// Returns command retcode and output (stdoud and stderr are mixed)
	RunAsyncCommand(service, cmd string) error
	// Returns content of the file from container by path
	GetFile(service, path string) (io.ReadCloser, error)
}

// DockerComposer is a Composer implementation based on docker and docker-compose
type DockerComposer struct {
	api         *client.Client
	containers  map[string]container.Summary
	stopped     map[string]struct{}
	projectName string
	config      string
}

// NewDockerComposer returns DockerComposer instance for specified compose file
// Parameter project specify prefix to distguish docker container and networks from different runs
func NewDockerComposer(project, config string) (*DockerComposer, error) {
	if config == "" {
		config = "docker-compose.yaml"
	}
	config, err := filepath.Abs(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build abs path to compose file: %s", err)
	}
	if project == "" {
		project = filepath.Base(filepath.Dir(config))
	}
	dc := new(DockerComposer)
	api, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker: %s", err)
	}
	dc.api = api
	dc.config = config
	dc.projectName = fmt.Sprintf("%s-%d", project, os.Getpid())
	dc.containers = make(map[string]container.Summary)
	dc.stopped = make(map[string]struct{})
	return dc, nil
}

func (dc *DockerComposer) runCompose(args []string, env []string) error {
	args2 := []string{"compose"}
	args2 = append(args2, "-f", dc.config, "-p", dc.projectName)
	args2 = append(args2, args...)
	cmd := exec.Command("docker", args2...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run 'docker compose %s': %s\n%s", strings.Join(args2, " "), err, out)
	}
	return nil
}

func (dc *DockerComposer) fillContainers() error {
	containers, err := dc.api.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return err
	}
	for _, c := range containers { // nolint: gocritic
		prj := c.Labels["com.docker.compose.project"]
		srv := c.Labels["com.docker.compose.service"]
		if prj != dc.projectName || srv == "" {
			continue
		}
		if c.State != "running" {
			if _, ok := dc.stopped[srv]; !ok {
				return fmt.Errorf("container %s is %s, not running", srv, c.State)
			}
		}
		dc.containers[srv] = c
	}
	return nil
}

// Up brings all containers up according to config
func (dc *DockerComposer) Up(env []string) error {
	err := dc.runCompose([]string{"up", "-d", "--force-recreate", "-t", strconv.Itoa(int(defaultDockerComposeTimeout / time.Second))}, env)
	if err != nil {
		// to save container logs
		_ = dc.fillContainers()
		return err
	}
	err = dc.fillContainers()
	return err
}

// Down trears all containers/VMs dowwn
func (dc *DockerComposer) Down() error {
	return dc.runCompose([]string{"down", "-v", "-t", strconv.Itoa(int(defaultDockerComposeTimeout / time.Second))}, nil)
}

// Services returns names/ids of running containers
func (dc *DockerComposer) Services() []string {
	services := make([]string, 0, len(dc.containers))
	for s := range dc.containers {
		services = append(services, s)
	}
	sort.Strings(services)
	return services
}

// GetAddr returns real exposed addr (ip:port) for given service/port
func (dc *DockerComposer) GetAddr(service string, port int) (string, error) {
	cont, ok := dc.containers[service]
	if !ok {
		return "", fmt.Errorf("no such service: %s", service)
	}
	for _, p := range cont.Ports {
		if int(p.PrivatePort) == port {
			return net.JoinHostPort(p.IP, strconv.Itoa(int(p.PublicPort))), nil
		}
	}
	return "", fmt.Errorf("service %s does not expose port %d", service, port)
}

// GetIp returns internal ip address of given service
func (dc *DockerComposer) GetIP(service string) (string, error) {
	cont, ok := dc.containers[service]
	if !ok {
		return "", fmt.Errorf("no such service: %s", service)
	}
	for _, network := range cont.NetworkSettings.Networks {
		return network.IPAddress, nil
	}
	return "", fmt.Errorf("no network for service: %s", service)
}

func (dc *DockerComposer) RunCommandAtHosts(cmd, hostSubstring string, timeout time.Duration) error {
	for name := range dc.containers {
		if !strings.Contains(name, hostSubstring) {
			continue
		}
		_, _, err := dc.RunCommand(name, cmd, timeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// RunCommand executes command inside container/VM with given timeout.
func (dc *DockerComposer) RunCommand(service string, cmd string, timeout time.Duration) (retcode int, out string, err error) {
	cont, ok := dc.containers[service]
	if !ok {
		return 0, "", fmt.Errorf("no such service: %s", service)
	}
	if timeout == 0 {
		timeout = defaultDockerTimeout + 3*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	execCfg := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{shell, "-c", cmd},
	}
	execResp, err := dc.api.ContainerExecCreate(ctx, cont.ID, execCfg)
	if err != nil {
		return 0, "", err
	}
	attachResp, err := dc.api.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return 0, "", err
	}
	output, err := io.ReadAll(attachResp.Reader)
	attachResp.Close()
	if err != nil {
		return 0, "", err
	}
	var insp container.ExecInspect
	Retry(func() bool {
		insp, err = dc.api.ContainerExecInspect(ctx, execResp.ID)
		return err != nil || !insp.Running
	}, timeout, time.Second)
	if err != nil {
		return 0, "", err
	}
	if insp.Running {
		return 0, "", fmt.Errorf("command %s didn't returned within %s", cmd, timeout)
	}
	return insp.ExitCode, string(output), nil
}

// RunAsyncCommand executes command inside container/VM without waiting for termination.
func (dc *DockerComposer) RunAsyncCommand(service string, cmd string) error {
	cont, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	execCfg := container.ExecOptions{
		Detach: true,
		Cmd:    []string{shell, "-c", cmd},
	}
	execResp, err := dc.api.ContainerExecCreate(context.Background(), cont.ID, execCfg)
	if err != nil {
		return err
	}
	return dc.api.ContainerExecStart(context.Background(), execResp.ID, container.ExecStartOptions{})
}

// GetFile returns content of the fail from container by path
func (dc *DockerComposer) GetFile(service, path string) (io.ReadCloser, error) {
	cont, ok := dc.containers[service]
	if !ok {
		return nil, fmt.Errorf("no such service: %s", service)
	}
	reader, _, err := dc.api.CopyFromContainer(context.Background(), cont.ID, path)
	if err != nil {
		return nil, err
	}
	return newUntarReaderCloser(reader)
}

// Start starts container by service name
func (dc *DockerComposer) Start(service string) error {
	cont, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultDockerTimeout)
	defer cancel()
	err := dc.api.ContainerRestart(ctx, cont.ID, container.StopOptions{})
	if err != nil {
		return err
	}
	delete(dc.stopped, service)
	// to update exposed ports
	return dc.fillContainers()
}

// Stop stops container by service name
func (dc *DockerComposer) Stop(service string) error {
	cont, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultDockerTimeout)
	defer cancel()
	err := dc.api.ContainerStop(ctx, cont.ID, container.StopOptions{})
	dc.stopped[service] = struct{}{}
	return err
}

// AttachToNet attaches container to network
func (dc *DockerComposer) AttachToNet(service string) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		"iptables -D INPUT -i eth0 -j DROP",
		"iptables -D OUTPUT -o eth0 -j DROP",
		"ip6tables -D INPUT -i eth0 -j DROP",
		"ip6tables -D OUTPUT -o eth0 -j DROP",
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// DetachFromNet detaches container from network
func (dc *DockerComposer) DetachFromNet(service string) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		"iptables -A INPUT -i eth0 -j DROP",
		"iptables -A OUTPUT -o eth0 -j DROP",
		"ip6tables -A INPUT -i eth0 -j DROP",
		"ip6tables -A OUTPUT -o eth0 -j DROP",
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// BlockPort blocks port for host
func (dc *DockerComposer) BlockPort(service string, port int) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		fmt.Sprintf("iptables -A INPUT -i eth0 -p tcp --dport %d -j DROP", port),
		fmt.Sprintf("iptables -A OUTPUT -o eth0 -p tcp --dport %d -j DROP", port),
		fmt.Sprintf("ip6tables -A INPUT -i eth0 -p tcp --dport %d -j DROP", port),
		fmt.Sprintf("ip6tables -A OUTPUT -o eth0 -p tcp --dport %d -j DROP", port),
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// BlockHostPort blocks host/port for host
func (dc *DockerComposer) BlockHostPort(service, host string, port int) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		fmt.Sprintf("iptables -A INPUT -i eth0 -s %s -p tcp --dport %d -j DROP", host, port),
		fmt.Sprintf("iptables -A OUTPUT -o eth0 -d %s -p tcp --dport %d -j DROP", host, port),
		fmt.Sprintf("ip6tables -A INPUT -i eth0 -s %s -p tcp --dport %d -j DROP", host, port),
		fmt.Sprintf("ip6tables -A OUTPUT -o eth0 -d %s -p tcp --dport %d -j DROP", host, port),
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// BlockHostConnections blocks incoming connections from specified host
func (dc *DockerComposer) BlockHostConnections(service, host string) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		fmt.Sprintf("iptables -A INPUT -s %s -p tcp --tcp-flags SYN,ACK SYN -j DROP", host),
		fmt.Sprintf("ip6tables -A INPUT -s %s -p tcp --tcp-flags SYN,ACK SYN -j DROP", host),
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// UnBlockPort removes blocking rules for port on host
func (dc *DockerComposer) UnBlockPort(service string, port int) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		fmt.Sprintf("iptables -D INPUT -i eth0 -p tcp --dport %d -j DROP", port),
		fmt.Sprintf("iptables -D OUTPUT -o eth0 -p tcp --dport %d -j DROP", port),
		fmt.Sprintf("ip6tables -D INPUT -i eth0 -p tcp --dport %d -j DROP", port),
		fmt.Sprintf("ip6tables -D OUTPUT -o eth0 -p tcp --dport %d -j DROP", port),
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// UnblockHostPort removes blocking rules for host/port for host
func (dc *DockerComposer) UnBlockHostPort(service, host string, port int) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		fmt.Sprintf("iptables -D INPUT -i eth0 -s %s -p tcp --dport %d -j DROP", host, port),
		fmt.Sprintf("iptables -D OUTPUT -o eth0 -d %s -p tcp --dport %d -j DROP", host, port),
		fmt.Sprintf("ip6tables -D INPUT -i eth0 -s %s -p tcp --dport %d -j DROP", host, port),
		fmt.Sprintf("ip6tables -D OUTPUT -o eth0 -d %s -p tcp --dport %d -j DROP", host, port),
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

// UnBlockHostConnections removes blocking incoming connections from host
func (dc *DockerComposer) UnBlockHostConnections(service, host string) error {
	_, ok := dc.containers[service]
	if !ok {
		return fmt.Errorf("no such service: %s", service)
	}
	cmds := []string{
		fmt.Sprintf("iptables -D INPUT -s %s -p tcp --tcp-flags SYN,ACK SYN -j DROP", host),
		fmt.Sprintf("ip6tables -D INPUT -s %s -p tcp --tcp-flags SYN,ACK SYN -j DROP", host),
	}
	for _, cmd := range cmds {
		_, _, err := dc.RunCommand(service, cmd, defaultDockerTimeout)
		if err != nil {
			return err
		}
	}
	return nil
}

func newUntarReaderCloser(reader io.ReadCloser) (io.ReadCloser, error) {
	tarReader := tar.NewReader(reader)
	_, err := tarReader.Next()
	if err != nil {
		return nil, err
	}
	return &untarReaderCloser{tarReader, reader}, nil
}

type untarReaderCloser struct {
	tarReader  *tar.Reader
	baseReader io.ReadCloser
}

func (usf untarReaderCloser) Read(b []byte) (int, error) {
	return usf.tarReader.Read(b)
}

func (usf untarReaderCloser) Close() error {
	return usf.baseReader.Close()
}
