package route

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	E "github.com/yusing/go-proxy/internal/error"
	url "github.com/yusing/go-proxy/internal/net/types"
	P "github.com/yusing/go-proxy/internal/proxy"
	PT "github.com/yusing/go-proxy/internal/proxy/fields"
	"github.com/yusing/go-proxy/internal/watcher/health"
)

type StreamRoute struct {
	*P.StreamEntry
	StreamImpl `json:"-"`

	url       url.URL
	healthMon health.HealthMonitor

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	connCh  chan any
	started atomic.Bool
	l       logrus.FieldLogger
}

type StreamImpl interface {
	Setup() error
	Accept() (any, error)
	Handle(conn any) error
	CloseListeners()
	String() string
}

func NewStreamRoute(entry *P.StreamEntry) (*StreamRoute, E.NestedError) {
	// TODO: support non-coherent scheme
	if !entry.Scheme.IsCoherent() {
		return nil, E.Unsupported("scheme", fmt.Sprintf("%v -> %v", entry.Scheme.ListeningScheme, entry.Scheme.ProxyScheme))
	}
	url, err := url.ParseURL(fmt.Sprintf("%s://%s:%d", entry.Scheme.ProxyScheme, entry.Host, entry.Port.ProxyPort))
	if err != nil {
		// !! should not happen
		panic(err)
	}
	base := &StreamRoute{
		StreamEntry: entry,
		url:         url,
		connCh:      make(chan any, 100),
	}
	if entry.Scheme.ListeningScheme.IsTCP() {
		base.StreamImpl = NewTCPRoute(base)
	} else {
		base.StreamImpl = NewUDPRoute(base)
	}
	if !entry.Healthcheck.Disabled {
		base.healthMon = health.NewRawHealthMonitor(base.ctx, string(entry.Alias), url, entry.Healthcheck)
	}
	base.l = logrus.WithField("route", base.StreamImpl)
	return base, nil
}

func (r *StreamRoute) String() string {
	return fmt.Sprintf("%s stream: %s", r.Scheme, r.Alias)
}

func (r *StreamRoute) URL() url.URL {
	return r.url
}

func (r *StreamRoute) Start() E.NestedError {
	if r.Port.ProxyPort == PT.NoPort || r.started.Load() {
		return nil
	}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.wg.Wait()
	if err := r.Setup(); err != nil {
		return E.FailWith("setup", err)
	}
	r.l.Infof("listening on port %d", r.Port.ListeningPort)
	r.started.Store(true)
	r.wg.Add(2)
	go r.grAcceptConnections()
	go r.grHandleConnections()
	if r.healthMon != nil {
		r.healthMon.Start()
	}
	return nil
}

func (r *StreamRoute) Stop() E.NestedError {
	if !r.started.Load() {
		return nil
	}
	r.started.Store(false)

	if r.healthMon != nil {
		r.healthMon.Stop()
	}

	r.cancel()
	r.CloseListeners()

	done := make(chan struct{}, 1)
	go func() {
		r.wg.Wait()
		close(done)
	}()

	timeout := time.After(streamStopListenTimeout)
	for {
		select {
		case <-done:
			r.l.Debug("stopped listening")
			return nil
		case <-timeout:
			return E.FailedWhy("stop", "timed out")
		}
	}
}

func (r *StreamRoute) Started() bool {
	return r.started.Load()
}

func (r *StreamRoute) grAcceptConnections() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
			conn, err := r.Accept()
			if err != nil {
				select {
				case <-r.ctx.Done():
					return
				default:
					r.l.Error(err)
					continue
				}
			}
			r.connCh <- conn
		}
	}
}

func (r *StreamRoute) grHandleConnections() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return
		case conn := <-r.connCh:
			go func() {
				err := r.Handle(conn)
				if err != nil && !errors.Is(err, context.Canceled) {
					r.l.Error(err)
				}
			}()
		}
	}
}
