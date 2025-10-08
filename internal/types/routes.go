package types

import (
	"net/http"

	"github.com/yusing/godoxy/agent/pkg/agent"
	"github.com/yusing/godoxy/internal/homepage"
	nettypes "github.com/yusing/godoxy/internal/net/types"
	provider "github.com/yusing/godoxy/internal/route/provider/types"
	"github.com/yusing/godoxy/internal/utils/pool"
	gperr "github.com/yusing/goutils/errs"
	"github.com/yusing/goutils/http/reverseproxy"
	"github.com/yusing/goutils/task"
)

type (
	Route interface {
		task.TaskStarter
		task.TaskFinisher
		pool.Object
		ProviderName() string
		GetProvider() RouteProvider
		TargetURL() *nettypes.URL
		HealthMonitor() HealthMonitor
		SetHealthMonitor(m HealthMonitor)
		References() []string

		Started() <-chan struct{}

		IdlewatcherConfig() *IdlewatcherConfig
		HealthCheckConfig() *HealthCheckConfig
		LoadBalanceConfig() *LoadBalancerConfig
		HomepageItem() homepage.Item
		DisplayName() string
		ContainerInfo() *Container

		GetAgent() *agent.AgentConfig

		IsDocker() bool
		IsAgent() bool
		UseLoadBalance() bool
		UseIdleWatcher() bool
		UseHealthCheck() bool
		UseAccessLog() bool
	}
	HTTPRoute interface {
		Route
		http.Handler
	}
	ReverseProxyRoute interface {
		HTTPRoute
		ReverseProxy() *reverseproxy.ReverseProxy
	}
	StreamRoute interface {
		Route
		nettypes.Stream
		Stream() nettypes.Stream
	}
	RouteProvider interface {
		Start(task.Parent) gperr.Error
		LoadRoutes() gperr.Error
		GetRoute(alias string) (r Route, ok bool)
		IterRoutes(yield func(alias string, r Route) bool)
		NumRoutes() int
		FindService(project, service string) (r Route, ok bool)
		Statistics() ProviderStats
		GetType() provider.Type
		ShortName() string
		String() string
	}
)
