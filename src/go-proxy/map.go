package main

import "sync"

type SafeMapInterface[KT comparable, VT interface{}] interface {
	Set(key KT, value VT)
	Ensure(key KT)
	Get(key KT) VT
	TryGet(key KT) (VT, bool)
	Clear()
	Size() int
	Contains(key KT) bool
	ForEach(fn func(key KT, value VT))
	Iterator() map[KT]VT
}

type SafeMap[KT comparable, VT interface{}] struct {
	SafeMapInterface[KT, VT]
	m map[KT]VT
	mutex sync.Mutex
	defaultFactory func() VT
}

func NewSafeMap[KT comparable, VT interface{}](df... func() VT) *SafeMap[KT, VT] {
	if len(df) == 0 {
		return &SafeMap[KT, VT]{
			m: make(map[KT]VT),
		}
	}
	return &SafeMap[KT, VT]{
		m: make(map[KT]VT),
		defaultFactory: df[0],
	}
}

func (m *SafeMap[KT, VT]) Set(key KT, value VT) {
	m.mutex.Lock()
	m.m[key] = value
	m.mutex.Unlock()
}

func (m *SafeMap[KT, VT]) Ensure(key KT) {
	m.mutex.Lock()
	if _, ok := m.m[key]; !ok {
		m.m[key] = m.defaultFactory()
	}
	m.mutex.Unlock()
}

func (m *SafeMap[KT, VT]) Get(key KT) VT {
	m.mutex.Lock()
	value := m.m[key]
	m.mutex.Unlock()
	return value
}

func (m *SafeMap[KT, VT]) TryGet(key KT) (VT, bool) {
	m.mutex.Lock()
	value, ok := m.m[key]
	m.mutex.Unlock()
	return value, ok
}

func (m *SafeMap[KT, VT]) Clear() {
	m.mutex.Lock()
	m.m = make(map[KT]VT)
	m.mutex.Unlock()
}

func (m *SafeMap[KT, VT]) Size() int {
	m.mutex.Lock()
	size := len(m.m)
	m.mutex.Unlock()
	return size
}

func (m *SafeMap[KT, VT]) Contains(key KT) bool {
	m.mutex.Lock()
	_, ok := m.m[key]
	m.mutex.Unlock()
	return ok
}

func (m *SafeMap[KT, VT]) ForEach(fn func(key KT, value VT)) {
	m.mutex.Lock()
	for k, v := range m.m {
		fn(k, v)
	}
	m.mutex.Unlock()
}

func (m *SafeMap[KT, VT]) Iterator() map[KT]VT {
	return m.m
}