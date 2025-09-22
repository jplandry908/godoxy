package idlewatcher

import (
	"net/http"

	nettypes "github.com/yusing/godoxy/internal/net/types"
	"github.com/yusing/godoxy/internal/types"
)

type Waker interface {
	types.HealthMonitor
	http.Handler
	nettypes.Stream
	Wake() error
}
