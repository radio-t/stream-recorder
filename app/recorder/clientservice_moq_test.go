// Code generated by moq; DO NOT EDIT.
// github.com/matryer/moq

package recorder

import (
	"context"
	"io"
	"sync"
)

// Ensure, that ClientServiceMock does implement ClientService.
// If this is not the case, regenerate this file with moq.
var _ ClientService = &ClientServiceMock{}

// ClientServiceMock is a mock implementation of ClientService.
//
//	func TestSomethingThatUsesClientService(t *testing.T) {
//
//		// make and configure a mocked ClientService
//		mockedClientService := &ClientServiceMock{
//			FetchLatestFunc: func(ctx context.Context) (string, error) {
//				panic("mock out the FetchLatest method")
//			},
//			FetchStreamFunc: func(ctx context.Context) (io.ReadCloser, error) {
//				panic("mock out the FetchStream method")
//			},
//		}
//
//		// use mockedClientService in code that requires ClientService
//		// and then make assertions.
//
//	}
type ClientServiceMock struct {
	// FetchLatestFunc mocks the FetchLatest method.
	FetchLatestFunc func(ctx context.Context) (string, error)

	// FetchStreamFunc mocks the FetchStream method.
	FetchStreamFunc func(ctx context.Context) (io.ReadCloser, error)

	// calls tracks calls to the methods.
	calls struct {
		// FetchLatest holds details about calls to the FetchLatest method.
		FetchLatest []struct {
			// Ctx is the ctx argument value.
			Ctx context.Context
		}
		// FetchStream holds details about calls to the FetchStream method.
		FetchStream []struct {
			// Ctx is the ctx argument value.
			Ctx context.Context
		}
	}
	lockFetchLatest sync.RWMutex
	lockFetchStream sync.RWMutex
}

// FetchLatest calls FetchLatestFunc.
func (mock *ClientServiceMock) FetchLatest(ctx context.Context) (string, error) {
	if mock.FetchLatestFunc == nil {
		panic("ClientServiceMock.FetchLatestFunc: method is nil but ClientService.FetchLatest was just called")
	}
	callInfo := struct {
		Ctx context.Context
	}{
		Ctx: ctx,
	}
	mock.lockFetchLatest.Lock()
	mock.calls.FetchLatest = append(mock.calls.FetchLatest, callInfo)
	mock.lockFetchLatest.Unlock()
	return mock.FetchLatestFunc(ctx)
}

// FetchLatestCalls gets all the calls that were made to FetchLatest.
// Check the length with:
//
//	len(mockedClientService.FetchLatestCalls())
func (mock *ClientServiceMock) FetchLatestCalls() []struct {
	Ctx context.Context
} {
	var calls []struct {
		Ctx context.Context
	}
	mock.lockFetchLatest.RLock()
	calls = mock.calls.FetchLatest
	mock.lockFetchLatest.RUnlock()
	return calls
}

// FetchStream calls FetchStreamFunc.
func (mock *ClientServiceMock) FetchStream(ctx context.Context) (io.ReadCloser, error) {
	if mock.FetchStreamFunc == nil {
		panic("ClientServiceMock.FetchStreamFunc: method is nil but ClientService.FetchStream was just called")
	}
	callInfo := struct {
		Ctx context.Context
	}{
		Ctx: ctx,
	}
	mock.lockFetchStream.Lock()
	mock.calls.FetchStream = append(mock.calls.FetchStream, callInfo)
	mock.lockFetchStream.Unlock()
	return mock.FetchStreamFunc(ctx)
}

// FetchStreamCalls gets all the calls that were made to FetchStream.
// Check the length with:
//
//	len(mockedClientService.FetchStreamCalls())
func (mock *ClientServiceMock) FetchStreamCalls() []struct {
	Ctx context.Context
} {
	var calls []struct {
		Ctx context.Context
	}
	mock.lockFetchStream.RLock()
	calls = mock.calls.FetchStream
	mock.lockFetchStream.RUnlock()
	return calls
}