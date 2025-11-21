package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImmutableMiddlewareChain(t *testing.T) {
	ctx := context.Background()

	t.Run("successful execution", func(t *testing.T) {
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return &MiddlewareResponse{
				Output:   "handler result",
				Metadata: make(map[string]interface{}),
				Headers:  make(map[string]string),
			}, nil
		}

		mw1 := NewMiddlewareFunc("mw1",
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				req.Metadata["mw1"] = "before"
				return req, nil
			},
			func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
				resp.Metadata["mw1"] = "after"
				return resp, nil
			},
			nil,
		)

		mw2 := NewMiddlewareFunc("mw2",
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				req.Metadata["mw2"] = "before"
				return req, nil
			},
			func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
				resp.Metadata["mw2"] = "after"
				return resp, nil
			},
			nil,
		)

		chain := NewImmutableMiddlewareChain(handler, mw1, mw2)

		req := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		resp, err := chain.Execute(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "handler result", resp.Output)
		assert.Equal(t, "after", resp.Metadata["mw1"])
		assert.Equal(t, "after", resp.Metadata["mw2"])
	})

	t.Run("error in OnBefore", func(t *testing.T) {
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return &MiddlewareResponse{Output: "handler result"}, nil
		}

		expectedErr := errors.New("before error")
		mw := NewMiddlewareFunc("mw",
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				return req, expectedErr
			},
			nil,
			func(ctx context.Context, err error) error {
				return err
			},
		)

		chain := NewImmutableMiddlewareChain(handler, mw)

		req := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		resp, err := chain.Execute(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("error in handler", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return nil, expectedErr
		}

		errorHandled := false
		mw := NewMiddlewareFunc("mw",
			nil,
			nil,
			func(ctx context.Context, err error) error {
				errorHandled = true
				return err
			},
		)

		chain := NewImmutableMiddlewareChain(handler, mw)

		req := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		resp, err := chain.Execute(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.True(t, errorHandled)
	})

	t.Run("middleware list immutable", func(t *testing.T) {
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return &MiddlewareResponse{Output: "result"}, nil
		}

		mw1 := &BaseMiddleware{name: "mw1"}
		mw2 := &BaseMiddleware{name: "mw2"}

		chain := NewImmutableMiddlewareChain(handler, mw1, mw2)

		middlewares := chain.Middlewares()
		assert.Len(t, middlewares, 2)

		// Verify we can't modify the original middleware list
		originalMiddlewares := []Middleware{mw1, mw2}
		originalMiddlewares[0] = &BaseMiddleware{name: "modified"}

		// Chain should still have the original middleware
		assert.Equal(t, "mw1", chain.Middlewares()[0].Name())
	})
}

func TestFastMiddlewareChain(t *testing.T) {
	ctx := context.Background()

	t.Run("successful execution", func(t *testing.T) {
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return &MiddlewareResponse{
				Output:   "handler result",
				Metadata: make(map[string]interface{}),
				Headers:  make(map[string]string),
			}, nil
		}

		mw1 := NewMiddlewareFunc("mw1",
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				req.Metadata["mw1"] = "before"
				return req, nil
			},
			nil,
			nil,
		)

		mw2 := NewMiddlewareFunc("mw2",
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				req.Metadata["mw2"] = "before"
				return req, nil
			},
			nil,
			nil,
		)

		chain := NewFastMiddlewareChain(handler, mw1, mw2)

		req := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		resp, err := chain.Execute(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "handler result", resp.Output)
	})

	t.Run("error in OnBefore", func(t *testing.T) {
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return &MiddlewareResponse{Output: "handler result"}, nil
		}

		expectedErr := errors.New("before error")
		mw := NewMiddlewareFunc("mw",
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				return req, expectedErr
			},
			nil,
			nil,
		)

		chain := NewFastMiddlewareChain(handler, mw)

		req := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		resp, err := chain.Execute(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("no OnAfter execution", func(t *testing.T) {
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return &MiddlewareResponse{
				Output:   "handler result",
				Metadata: make(map[string]interface{}),
				Headers:  make(map[string]string),
			}, nil
		}

		afterCalled := false
		mw := NewMiddlewareFunc("mw",
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				req.Metadata["before"] = "called"
				return req, nil
			},
			func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
				afterCalled = true
				return resp, nil
			},
			nil,
		)

		chain := NewFastMiddlewareChain(handler, mw)

		req := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		resp, err := chain.Execute(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.False(t, afterCalled, "OnAfter should not be called in FastMiddlewareChain")
	})

	t.Run("middleware list", func(t *testing.T) {
		handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return &MiddlewareResponse{Output: "result"}, nil
		}

		mw1 := &BaseMiddleware{name: "mw1"}
		mw2 := &BaseMiddleware{name: "mw2"}

		chain := NewFastMiddlewareChain(handler, mw1, mw2)

		middlewares := chain.Middlewares()
		assert.Len(t, middlewares, 2)
		assert.Equal(t, "mw1", middlewares[0].Name())
		assert.Equal(t, "mw2", middlewares[1].Name())
	})
}

func TestImmutableMiddlewareChainVsOriginal(t *testing.T) {
	ctx := context.Background()

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{
			Output:   req.Input,
			Metadata: make(map[string]interface{}),
			Headers:  make(map[string]string),
		}, nil
	}

	// Create middleware that modifies request and response
	mw1 := NewMiddlewareFunc("mw1",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			req.Metadata["mw1_before"] = true
			return req, nil
		},
		func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
			resp.Metadata["mw1_after"] = true
			return resp, nil
		},
		nil,
	)

	mw2 := NewMiddlewareFunc("mw2",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			req.Metadata["mw2_before"] = true
			return req, nil
		},
		func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
			resp.Metadata["mw2_after"] = true
			return resp, nil
		},
		nil,
	)

	req := &MiddlewareRequest{
		Input:     "test",
		Metadata:  make(map[string]interface{}),
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	t.Run("original chain", func(t *testing.T) {
		chain := NewMiddlewareChain(handler)
		chain.Use(mw1, mw2)

		resp, err := chain.Execute(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Metadata["mw1_after"].(bool))
		assert.True(t, resp.Metadata["mw2_after"].(bool))
	})

	t.Run("immutable chain", func(t *testing.T) {
		chain := NewImmutableMiddlewareChain(handler, mw1, mw2)

		resp, err := chain.Execute(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Metadata["mw1_after"].(bool))
		assert.True(t, resp.Metadata["mw2_after"].(bool))
	})

	t.Run("results match", func(t *testing.T) {
		originalChain := NewMiddlewareChain(handler)
		originalChain.Use(mw1, mw2)

		immutableChain := NewImmutableMiddlewareChain(handler, mw1, mw2)

		req1 := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		req2 := &MiddlewareRequest{
			Input:     "test",
			Metadata:  make(map[string]interface{}),
			Headers:   make(map[string]string),
			Timestamp: time.Now(),
		}

		resp1, err1 := originalChain.Execute(ctx, req1)
		resp2, err2 := immutableChain.Execute(ctx, req2)

		assert.Equal(t, err1, err2)
		assert.Equal(t, resp1.Output, resp2.Output)
		assert.Equal(t, resp1.Metadata["mw1_after"], resp2.Metadata["mw1_after"])
		assert.Equal(t, resp1.Metadata["mw2_after"], resp2.Metadata["mw2_after"])
	})
}
