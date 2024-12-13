package monitor

import (
	"github.com/yusing/go-proxy/internal/docker"
	"github.com/yusing/go-proxy/internal/net/types"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/yusing/go-proxy/internal/watcher/health"
)

type DockerHealthMonitor struct {
	*monitor
	client      *docker.SharedClient
	containerID string
	fallback    health.HealthChecker
}

func NewDockerHealthMonitor(client *docker.SharedClient, containerID string, config *health.HealthCheckConfig, fallback health.HealthChecker) *DockerHealthMonitor {
	mon := new(DockerHealthMonitor)
	mon.client = client
	mon.containerID = containerID
	mon.monitor = newMonitor(types.URL{}, config, mon.CheckHealth)
	mon.fallback = fallback
	return mon
}

func (mon *DockerHealthMonitor) CheckHealth() (result *health.HealthCheckResult, err error) {
	cont, err := mon.client.ContainerInspect(mon.task.Context(), mon.containerID)
	if err != nil {
		return mon.fallback.CheckHealth()
	}
	if cont.State.Health == nil {
		return mon.fallback.CheckHealth()
	}
	result = new(health.HealthCheckResult)
	result.Healthy = cont.State.Health.Status == dockerTypes.Healthy
	if len(cont.State.Health.Log) > 0 {
		lastLog := cont.State.Health.Log[len(cont.State.Health.Log)-1]
		result.Detail = lastLog.Output
		result.Latency = lastLog.End.Sub(lastLog.Start)
	}
	return
}
