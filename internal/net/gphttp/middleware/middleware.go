package middleware

import (
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/yusing/godoxy/internal/gperr"
	gphttp "github.com/yusing/godoxy/internal/net/gphttp"
	"github.com/yusing/godoxy/internal/serialization"
	"github.com/yusing/goutils/http/reverseproxy"
)

type (
	Error = gperr.Error

	ReverseProxy = reverseproxy.ReverseProxy
	ProxyRequest = reverseproxy.ProxyRequest

	ImplNewFunc = func() any
	OptionsRaw  = map[string]any

	commonOptions = struct {
		// priority is only applied for ReverseProxy.
		//
		// Middleware compose follows the order of the slice
		//
		// Default is 10, 0 is the highest
		Priority int    `json:"priority"`
		Bypass   Bypass `json:"bypass"`
	}

	Middleware struct {
		name      string
		construct ImplNewFunc
		impl      any

		commonOptions
	}
	ByPriority []*Middleware

	RequestModifier interface {
		before(w http.ResponseWriter, r *http.Request) (proceed bool)
	}
	ResponseModifier             interface{ modifyResponse(r *http.Response) error }
	MiddlewareWithSetup          interface{ setup() }
	MiddlewareFinalizer          interface{ finalize() }
	MiddlewareFinalizerWithError interface {
		finalize() error
	}
)

const DefaultPriority = 10

func (m ByPriority) Len() int           { return len(m) }
func (m ByPriority) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m ByPriority) Less(i, j int) bool { return m[i].Priority < m[j].Priority }

func NewMiddleware[ImplType any]() *Middleware {
	// type check
	t := any(new(ImplType))
	switch t.(type) {
	case RequestModifier:
	case ResponseModifier:
	default:
		panic("must implement RequestModifier or ResponseModifier")
	}
	_, hasFinializer := t.(MiddlewareFinalizer)
	_, hasFinializerWithError := t.(MiddlewareFinalizerWithError)
	if hasFinializer && hasFinializerWithError {
		panic("MiddlewareFinalizer and MiddlewareFinalizerWithError are mutually exclusive")
	}
	return &Middleware{
		name:      strings.ToLower(reflect.TypeFor[ImplType]().Name()),
		construct: func() any { return new(ImplType) },
	}
}

func (m *Middleware) setup() {
	if setup, ok := m.impl.(MiddlewareWithSetup); ok {
		setup.setup()
	}
}

func (m *Middleware) apply(optsRaw OptionsRaw) gperr.Error {
	if len(optsRaw) == 0 {
		return nil
	}
	commonOpts := map[string]any{}
	if priority, ok := optsRaw["priority"]; ok {
		commonOpts["priority"] = priority
	}
	if bypass, ok := optsRaw["bypass"]; ok {
		commonOpts["bypass"] = bypass
	}
	if len(commonOpts) > 0 {
		if err := serialization.MapUnmarshalValidate(commonOpts, &m.commonOptions); err != nil {
			return err
		}
		optsRaw = maps.Clone(optsRaw)
		for k := range commonOpts {
			delete(optsRaw, k)
		}
	}
	return serialization.MapUnmarshalValidate(optsRaw, m.impl)
}

func (m *Middleware) finalize() error {
	if finalizer, ok := m.impl.(MiddlewareFinalizer); ok {
		finalizer.finalize()
	}
	if finalizer, ok := m.impl.(MiddlewareFinalizerWithError); ok {
		return finalizer.finalize()
	}
	return nil
}

func (m *Middleware) New(optsRaw OptionsRaw) (*Middleware, gperr.Error) {
	if m.construct == nil { // likely a middleware from compose
		if len(optsRaw) != 0 {
			return nil, gperr.New("additional options not allowed for middleware ").Subject(m.name)
		}
		return m, nil
	}
	mid := &Middleware{name: m.name, impl: m.construct()}
	mid.setup()
	if err := mid.apply(optsRaw); err != nil {
		return nil, err
	}
	if err := mid.finalize(); err != nil {
		return nil, gperr.Wrap(err)
	}
	mid.impl = mid.withCheckBypass()
	return mid, nil
}

func (m *Middleware) Name() string {
	return m.name
}

func (m *Middleware) String() string {
	return m.name
}

func (m *Middleware) MarshalJSON() ([]byte, error) {
	type allOptions struct {
		commonOptions
		any
	}
	return json.MarshalIndent(map[string]any{
		"name": m.name,
		"options": allOptions{
			commonOptions: m.commonOptions,
			any:           m.impl,
		},
	}, "", "  ")
}

func (m *Middleware) ModifyRequest(next http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	if exec, ok := m.impl.(RequestModifier); ok {
		if proceed := exec.before(w, r); !proceed {
			return
		}
	}
	next(w, r)
}

func (m *Middleware) ModifyResponse(resp *http.Response) error {
	if exec, ok := m.impl.(ResponseModifier); ok {
		return exec.modifyResponse(resp)
	}
	return nil
}

func (m *Middleware) ServeHTTP(next http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	if exec, ok := m.impl.(ResponseModifier); ok {
		w = gphttp.NewModifyResponseWriter(w, r, func(resp *http.Response) error {
			return exec.modifyResponse(resp)
		})
	}
	if exec, ok := m.impl.(RequestModifier); ok {
		if proceed := exec.before(w, r); !proceed {
			return
		}
	}
	next(w, r)
}

func (m *Middleware) LogWarn(req *http.Request) *zerolog.Event {
	return log.Warn().Str("middleware", m.name).
		Str("host", req.Host).
		Str("path", req.URL.Path)
}

func (m *Middleware) LogError(req *http.Request) *zerolog.Event {
	return log.Error().Str("middleware", m.name).
		Str("host", req.Host).
		Str("path", req.URL.Path)
}

func PatchReverseProxy(rp *ReverseProxy, middlewaresMap map[string]OptionsRaw) (err gperr.Error) {
	var middlewares []*Middleware
	middlewares, err = compileMiddlewares(middlewaresMap)
	if err != nil {
		return
	}
	patchReverseProxy(rp, middlewares)
	return
}

func patchReverseProxy(rp *ReverseProxy, middlewares []*Middleware) {
	sort.Sort(ByPriority(middlewares))

	mid := NewMiddlewareChain(rp.TargetName, middlewares)

	if before, ok := mid.impl.(RequestModifier); ok {
		next := rp.HandlerFunc
		rp.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
			if proceed := before.before(w, r); proceed {
				next(w, r)
			}
		}
	}

	if mr, ok := mid.impl.(ResponseModifier); ok {
		if rp.ModifyResponse != nil {
			ori := rp.ModifyResponse
			rp.ModifyResponse = func(res *http.Response) error {
				if err := mr.modifyResponse(res); err != nil {
					return err
				}
				return ori(res)
			}
		} else {
			rp.ModifyResponse = mr.modifyResponse
		}
	}
}
