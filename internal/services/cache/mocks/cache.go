package mocks

import "wallet-transfer-assignment/internal/services/cache"

// MockCacheHelper is a manual mock for cache.CacheHelper
type MockCacheHelper struct {
	SetFunc                 func(key string, value cache.CacheItem)
	GetFunc                 func(key string) (cache.CacheItem, bool)
	DeleteFunc              func(key string)
	CompareAndSwapStateFunc func(key string, oldState, newState cache.IdempotencyState) bool
	ClearFunc               func()
}

func (m *MockCacheHelper) Clear() {
	if m.ClearFunc != nil {
		m.ClearFunc()
	}
}

func (m *MockCacheHelper) Set(key string, value cache.CacheItem) {
	if m.SetFunc != nil {
		m.SetFunc(key, value)
	}
}

func (m *MockCacheHelper) Get(key string) (cache.CacheItem, bool) {
	if m.GetFunc != nil {
		return m.GetFunc(key)
	}
	return cache.CacheItem{}, false
}

func (m *MockCacheHelper) Delete(key string) {
	if m.DeleteFunc != nil {
		m.DeleteFunc(key)
	}
}

func (m *MockCacheHelper) CompareAndSwapState(key string, oldState, newState cache.IdempotencyState) bool {
	if m.CompareAndSwapStateFunc != nil {
		return m.CompareAndSwapStateFunc(key, oldState, newState)
	}
	return false
}
