// path: internal/executor/mock_client.go
package executor

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"
)

type mockDockerClient struct {
	mu sync.Mutex

	// Injected errors and status codes
	createErr      error
	startErr       error
	waitErr        error
	removeErr      error
	waitStatusCode int64
	waitDelay      time.Duration
	logContent     string

	// Captured state for assertions
	createCalled       bool
	startCalled        bool
	waitCalled         bool
	removeCalled       bool
	logsCalled         bool
	listCalled         bool
	lastConfig         *ContainerConfig
	lastHostConfig     *HostConfig
	removedContainerID string
	containers         []ContainerSummary
}

func newMockDockerClient() *mockDockerClient {
	return &mockDockerClient{
		waitStatusCode: 0,
	}
}

// NewMockDockerClient creates a mock Docker client implementing DockerClientAPI.
func NewMockDockerClient() DockerClientAPI {
	return newMockDockerClient()
}

func (m *mockDockerClient) ContainerCreate(ctx context.Context,
	config *ContainerConfig,
	hostConfig *HostConfig,
	containerName string) (ContainerCreateCreatedBody, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalled = true
	m.lastConfig = config
	m.lastHostConfig = hostConfig

	if m.createErr != nil {
		return ContainerCreateCreatedBody{}, m.createErr
	}
	return ContainerCreateCreatedBody{ID: "mock-container-12345"}, nil
}

func (m *mockDockerClient) ContainerStart(ctx context.Context, containerID string, options ContainerStartOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled = true
	return m.startErr
}

func (m *mockDockerClient) ContainerWait(ctx context.Context, containerID string, condition ContainerWaitCondition) (<-chan ContainerWaitResponse, <-chan error) {
	m.mu.Lock()
	m.waitCalled = true
	delay := m.waitDelay
	status := m.waitStatusCode
	injectedErr := m.waitErr
	m.mu.Unlock()

	waitCh := make(chan ContainerWaitResponse, 1)
	errCh := make(chan error, 1)

	go func() {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				errCh <- ctx.Err()
				close(waitCh)
				close(errCh)
				return
			}
		}

		if injectedErr != nil {
			errCh <- injectedErr
		} else {
			waitCh <- ContainerWaitResponse{StatusCode: status}
		}
		close(waitCh)
		close(errCh)
	}()

	return waitCh, errCh
}

func (m *mockDockerClient) ContainerRemove(ctx context.Context, containerID string, options ContainerRemoveOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeCalled = true
	m.removedContainerID = containerID
	return m.removeErr
}

func (m *mockDockerClient) ContainerLogs(ctx context.Context, containerID string, options ContainerLogsOptions) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logsCalled = true
	return io.NopCloser(strings.NewReader(m.logContent)), nil
}

func (m *mockDockerClient) ContainerList(ctx context.Context, options ContainerListOptions) ([]ContainerSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalled = true
	return m.containers, nil
}
