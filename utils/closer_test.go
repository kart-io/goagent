package utils

import (
	"errors"
	"io"
	"testing"
)

// mockCloser 模拟 io.Closer 用于测试
type mockCloser struct {
	closed    bool
	shouldErr bool
	err       error
}

func (m *mockCloser) Close() error {
	m.closed = true
	if m.shouldErr {
		return m.err
	}
	return nil
}

func TestCloseQuietly(t *testing.T) {
	t.Run("正常关闭", func(t *testing.T) {
		mc := &mockCloser{}
		CloseQuietly(mc)
		if !mc.closed {
			t.Error("期望 closer 被关闭")
		}
	})

	t.Run("nil closer 不 panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("不应该 panic: %v", r)
			}
		}()
		CloseQuietly(nil)
	})

	t.Run("忽略关闭错误", func(t *testing.T) {
		mc := &mockCloser{shouldErr: true, err: errors.New("close error")}
		CloseQuietly(mc) // 不应该 panic
		if !mc.closed {
			t.Error("期望 closer 被关闭")
		}
	})
}

func TestCloseWithLog(t *testing.T) {
	t.Run("成功关闭不调用日志", func(t *testing.T) {
		mc := &mockCloser{}
		logCalled := false
		CloseWithLog(mc, func(err error) {
			logCalled = true
		})
		if !mc.closed {
			t.Error("期望 closer 被关闭")
		}
		if logCalled {
			t.Error("成功时不应该调用日志函数")
		}
	})

	t.Run("失败时调用日志", func(t *testing.T) {
		expectedErr := errors.New("close error")
		mc := &mockCloser{shouldErr: true, err: expectedErr}
		var loggedErr error
		CloseWithLog(mc, func(err error) {
			loggedErr = err
		})
		if !mc.closed {
			t.Error("期望 closer 被关闭")
		}
		if loggedErr != expectedErr {
			t.Errorf("期望错误 %v，得到 %v", expectedErr, loggedErr)
		}
	})

	t.Run("nil closer 不 panic", func(t *testing.T) {
		CloseWithLog(nil, func(err error) {})
	})

	t.Run("nil logFn 不 panic", func(t *testing.T) {
		mc := &mockCloser{shouldErr: true, err: errors.New("error")}
		CloseWithLog(mc, nil)
	})
}

func TestCloseWithError(t *testing.T) {
	t.Run("成功关闭不设置错误", func(t *testing.T) {
		mc := &mockCloser{}
		var err error
		CloseWithError(mc, &err)
		if err != nil {
			t.Errorf("期望 err 为 nil，得到 %v", err)
		}
	})

	t.Run("失败时设置错误", func(t *testing.T) {
		expectedErr := errors.New("close error")
		mc := &mockCloser{shouldErr: true, err: expectedErr}
		var err error
		CloseWithError(mc, &err)
		if err != expectedErr {
			t.Errorf("期望错误 %v，得到 %v", expectedErr, err)
		}
	})

	t.Run("合并已有错误", func(t *testing.T) {
		closeErr := errors.New("close error")
		originalErr := errors.New("original error")
		mc := &mockCloser{shouldErr: true, err: closeErr}
		err := originalErr
		CloseWithError(mc, &err)
		if !errors.Is(err, originalErr) {
			t.Error("期望包含原始错误")
		}
		if !errors.Is(err, closeErr) {
			t.Error("期望包含关闭错误")
		}
	})

	t.Run("nil closer 不 panic", func(t *testing.T) {
		var err error
		CloseWithError(nil, &err)
	})

	t.Run("nil errp 不 panic", func(t *testing.T) {
		mc := &mockCloser{shouldErr: true, err: errors.New("error")}
		CloseWithError(mc, nil)
	})
}

func TestCloserFunc(t *testing.T) {
	t.Run("执行函数", func(t *testing.T) {
		called := false
		cf := CloserFunc(func() error {
			called = true
			return nil
		})
		err := cf.Close()
		if err != nil {
			t.Errorf("期望 nil 错误，得到 %v", err)
		}
		if !called {
			t.Error("期望函数被调用")
		}
	})

	t.Run("返回错误", func(t *testing.T) {
		expectedErr := errors.New("func error")
		cf := CloserFunc(func() error {
			return expectedErr
		})
		err := cf.Close()
		if err != expectedErr {
			t.Errorf("期望错误 %v，得到 %v", expectedErr, err)
		}
	})

	t.Run("nil 函数不 panic", func(t *testing.T) {
		var cf CloserFunc
		err := cf.Close()
		if err != nil {
			t.Errorf("期望 nil 错误，得到 %v", err)
		}
	})

	t.Run("实现 io.Closer 接口", func(t *testing.T) {
		cf := CloserFunc(func() error { return nil })
		var _ io.Closer = cf
	})
}

func TestMultiCloser(t *testing.T) {
	t.Run("关闭所有资源", func(t *testing.T) {
		mc1 := &mockCloser{}
		mc2 := &mockCloser{}
		mc3 := &mockCloser{}

		multi := NewMultiCloser(mc1, mc2, mc3)
		err := multi.Close()
		if err != nil {
			t.Errorf("期望 nil 错误，得到 %v", err)
		}
		if !mc1.closed || !mc2.closed || !mc3.closed {
			t.Error("期望所有 closer 被关闭")
		}
	})

	t.Run("收集所有错误", func(t *testing.T) {
		err1 := errors.New("error 1")
		err2 := errors.New("error 2")
		mc1 := &mockCloser{shouldErr: true, err: err1}
		mc2 := &mockCloser{}
		mc3 := &mockCloser{shouldErr: true, err: err2}

		multi := NewMultiCloser(mc1, mc2, mc3)
		err := multi.Close()

		if !errors.Is(err, err1) {
			t.Error("期望包含 error 1")
		}
		if !errors.Is(err, err2) {
			t.Error("期望包含 error 2")
		}
	})

	t.Run("Add 添加 closer", func(t *testing.T) {
		mc1 := &mockCloser{}
		mc2 := &mockCloser{}

		multi := NewMultiCloser(mc1)
		multi.Add(mc2)
		multi.Add(nil) // nil 应被忽略
		_ = multi.Close()

		if !mc1.closed || !mc2.closed {
			t.Error("期望所有 closer 被关闭")
		}
	})

	t.Run("跳过 nil closer", func(t *testing.T) {
		mc := &mockCloser{}
		multi := NewMultiCloser(nil, mc, nil)
		err := multi.Close()
		if err != nil {
			t.Errorf("期望 nil 错误，得到 %v", err)
		}
		if !mc.closed {
			t.Error("期望 closer 被关闭")
		}
	})

	t.Run("实现 io.Closer 接口", func(t *testing.T) {
		multi := NewMultiCloser()
		var _ io.Closer = multi
	})
}

// 演示实际使用场景的示例测试
func TestCloseWithError_RealWorldUsage(t *testing.T) {
	// 模拟一个处理函数，使用 CloseWithError 捕获关闭错误
	processResource := func(shouldFail bool) (err error) {
		mc := &mockCloser{shouldErr: shouldFail, err: errors.New("close failed")}
		defer CloseWithError(mc, &err)

		// 模拟一些处理逻辑
		return nil
	}

	t.Run("处理成功且关闭成功", func(t *testing.T) {
		err := processResource(false)
		if err != nil {
			t.Errorf("期望 nil 错误，得到 %v", err)
		}
	})

	t.Run("处理成功但关闭失败", func(t *testing.T) {
		err := processResource(true)
		if err == nil {
			t.Error("期望有错误")
		}
	})
}
