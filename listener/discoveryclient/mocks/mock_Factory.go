// Code generated by mockery v2.52.3. DO NOT EDIT.

package mocks

import (
	discovery "k8s.io/client-go/discovery"

	meta "k8s.io/apimachinery/pkg/api/meta"

	mock "github.com/stretchr/testify/mock"
)

// MockFactory is an autogenerated mock type for the Factory type
type MockFactory struct {
	mock.Mock
}

type MockFactory_Expecter struct {
	mock *mock.Mock
}

func (_m *MockFactory) EXPECT() *MockFactory_Expecter {
	return &MockFactory_Expecter{mock: &_m.Mock}
}

// ClientForCluster provides a mock function with given fields: name
func (_m *MockFactory) ClientForCluster(name string) (discovery.DiscoveryInterface, error) {
	ret := _m.Called(name)

	if len(ret) == 0 {
		panic("no return value specified for ClientForCluster")
	}

	var r0 discovery.DiscoveryInterface
	var r1 error
	if rf, ok := ret.Get(0).(func(string) (discovery.DiscoveryInterface, error)); ok {
		return rf(name)
	}
	if rf, ok := ret.Get(0).(func(string) discovery.DiscoveryInterface); ok {
		r0 = rf(name)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(discovery.DiscoveryInterface)
		}
	}

	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(name)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockFactory_ClientForCluster_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'ClientForCluster'
type MockFactory_ClientForCluster_Call struct {
	*mock.Call
}

// ClientForCluster is a helper method to define mock.On call
//   - name string
func (_e *MockFactory_Expecter) ClientForCluster(name interface{}) *MockFactory_ClientForCluster_Call {
	return &MockFactory_ClientForCluster_Call{Call: _e.mock.On("ClientForCluster", name)}
}

func (_c *MockFactory_ClientForCluster_Call) Run(run func(name string)) *MockFactory_ClientForCluster_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string))
	})
	return _c
}

func (_c *MockFactory_ClientForCluster_Call) Return(_a0 discovery.DiscoveryInterface, _a1 error) *MockFactory_ClientForCluster_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockFactory_ClientForCluster_Call) RunAndReturn(run func(string) (discovery.DiscoveryInterface, error)) *MockFactory_ClientForCluster_Call {
	_c.Call.Return(run)
	return _c
}

// RestMapperForCluster provides a mock function with given fields: name
func (_m *MockFactory) RestMapperForCluster(name string) (meta.RESTMapper, error) {
	ret := _m.Called(name)

	if len(ret) == 0 {
		panic("no return value specified for RestMapperForCluster")
	}

	var r0 meta.RESTMapper
	var r1 error
	if rf, ok := ret.Get(0).(func(string) (meta.RESTMapper, error)); ok {
		return rf(name)
	}
	if rf, ok := ret.Get(0).(func(string) meta.RESTMapper); ok {
		r0 = rf(name)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(meta.RESTMapper)
		}
	}

	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(name)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockFactory_RestMapperForCluster_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'RestMapperForCluster'
type MockFactory_RestMapperForCluster_Call struct {
	*mock.Call
}

// RestMapperForCluster is a helper method to define mock.On call
//   - name string
func (_e *MockFactory_Expecter) RestMapperForCluster(name interface{}) *MockFactory_RestMapperForCluster_Call {
	return &MockFactory_RestMapperForCluster_Call{Call: _e.mock.On("RestMapperForCluster", name)}
}

func (_c *MockFactory_RestMapperForCluster_Call) Run(run func(name string)) *MockFactory_RestMapperForCluster_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string))
	})
	return _c
}

func (_c *MockFactory_RestMapperForCluster_Call) Return(_a0 meta.RESTMapper, _a1 error) *MockFactory_RestMapperForCluster_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockFactory_RestMapperForCluster_Call) RunAndReturn(run func(string) (meta.RESTMapper, error)) *MockFactory_RestMapperForCluster_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockFactory creates a new instance of MockFactory. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockFactory(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockFactory {
	mock := &MockFactory{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
