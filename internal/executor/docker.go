// path: internal/executor/docker.go
package executor

import (
	"context"
	"io"
)

// ContainerConfig defines configuration options for creating a container.
type ContainerConfig struct {
	Image          string
	Cmd            []string
	Env            []string
	WorkingDir     string
	ReadonlyRootfs bool
	Labels         map[string]string
}

// Resources specifies resource constraints for a container.
type Resources struct {
	NanoCPUs int64
	Memory   int64
}

// HostConfig defines host-specific container configurations.
type HostConfig struct {
	AutoRemove  bool
	NetworkMode string
	Privileged  bool
	Binds       []string
	Tmpfs       map[string]string
	SecurityOpt []string
	Resources   Resources
}

// ContainerCreateCreatedBody holds the response after container creation.
type ContainerCreateCreatedBody struct {
	ID string
}

// ContainerStartOptions holds options for container start.
type ContainerStartOptions struct{}

// ContainerWaitCondition defines the state condition to wait for.
type ContainerWaitCondition string

const (
	ContainerWaitConditionNotRunning ContainerWaitCondition = "not-running"
)

// ContainerWaitResponse represents the result of container exit.
type ContainerWaitResponse struct {
	StatusCode int64
}

// ContainerRemoveOptions holds options for container removal.
type ContainerRemoveOptions struct {
	Force         bool
	RemoveVolumes bool
}

// ContainerLogsOptions holds options for streaming logs.
type ContainerLogsOptions struct {
	ShowStdout bool
	ShowStderr bool
	Follow     bool
	Timestamps bool
}

// ContainerSummary represents container metadata for janitor operations.
type ContainerSummary struct {
	ID     string
	Names  []string
	Labels map[string]string
}

// ContainerListOptions specifies options for listing containers.
type ContainerListOptions struct {
	All    bool
	Labels map[string]string
}

// DockerClientAPI abstracts the Docker Engine SDK methods used by the executor.
type DockerClientAPI interface {
	ContainerCreate(ctx context.Context,
		config *ContainerConfig,
		hostConfig *HostConfig,
		containerName string) (ContainerCreateCreatedBody, error)
	ContainerStart(ctx context.Context, containerID string, options ContainerStartOptions) error
	ContainerWait(ctx context.Context, containerID string, condition ContainerWaitCondition) (<-chan ContainerWaitResponse, <-chan error)
	ContainerRemove(ctx context.Context, containerID string, options ContainerRemoveOptions) error
	ContainerLogs(ctx context.Context, containerID string, options ContainerLogsOptions) (io.ReadCloser, error)
	ContainerList(ctx context.Context, options ContainerListOptions) ([]ContainerSummary, error)
}

