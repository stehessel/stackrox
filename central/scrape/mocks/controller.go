// Code generated by MockGen. DO NOT EDIT.
// Source: controller.go

// Package mocks is a generated GoMock package.
package mocks

import (
	gomock "github.com/golang/mock/gomock"
	central "github.com/stackrox/rox/generated/internalapi/central"
	compliance "github.com/stackrox/rox/generated/internalapi/compliance"
	concurrency "github.com/stackrox/rox/pkg/concurrency"
	set "github.com/stackrox/rox/pkg/set"
	reflect "reflect"
)

// MockController is a mock of Controller interface
type MockController struct {
	ctrl     *gomock.Controller
	recorder *MockControllerMockRecorder
}

// MockControllerMockRecorder is the mock recorder for MockController
type MockControllerMockRecorder struct {
	mock *MockController
}

// NewMockController creates a new mock instance
func NewMockController(ctrl *gomock.Controller) *MockController {
	mock := &MockController{ctrl: ctrl}
	mock.recorder = &MockControllerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockController) EXPECT() *MockControllerMockRecorder {
	return m.recorder
}

// ProcessScrapeUpdate mocks base method
func (m *MockController) ProcessScrapeUpdate(update *central.ScrapeUpdate) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessScrapeUpdate", update)
	ret0, _ := ret[0].(error)
	return ret0
}

// ProcessScrapeUpdate indicates an expected call of ProcessScrapeUpdate
func (mr *MockControllerMockRecorder) ProcessScrapeUpdate(update interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessScrapeUpdate", reflect.TypeOf((*MockController)(nil).ProcessScrapeUpdate), update)
}

// RunScrape mocks base method
func (m *MockController) RunScrape(expectedHosts set.StringSet, kill concurrency.Waitable, standardIDs []string) (map[string]*compliance.ComplianceReturn, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunScrape", expectedHosts, kill, standardIDs)
	ret0, _ := ret[0].(map[string]*compliance.ComplianceReturn)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RunScrape indicates an expected call of RunScrape
func (mr *MockControllerMockRecorder) RunScrape(expectedHosts, kill, standardIDs interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunScrape", reflect.TypeOf((*MockController)(nil).RunScrape), expectedHosts, kill, standardIDs)
}
