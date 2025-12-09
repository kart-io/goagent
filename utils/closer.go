package utils

import (
	"errors"
	"io"
)

// Closer 提供统一的资源关闭处理工具
//
// 解决 Go 中 defer xxx.Close() 不检查错误返回值的 lint 问题
// 提供多种策略：静默关闭、日志记录、错误捕获

// CloseQuietly 静默关闭资源，忽略错误
//
// 适用于测试代码或确定不需要处理关闭错误的场景
//
// 用法:
//
//	defer utils.CloseQuietly(file)
func CloseQuietly(closer io.Closer) {
	if closer != nil {
		_ = closer.Close()
	}
}

// CloseWithLog 关闭资源并在出错时调用日志函数
//
// 适用于生产代码中需要记录关闭错误但不影响主流程的场景
//
// 用法:
//
//	defer utils.CloseWithLog(file, func(err error) {
//	    log.Error("failed to close file", "error", err)
//	})
func CloseWithLog(closer io.Closer, logFn func(error)) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil && logFn != nil {
		logFn(err)
	}
}

// CloseWithError 关闭资源并将错误合并到指定的错误指针
//
// 适用于需要将关闭错误与函数返回错误合并的场景
// 如果原错误为 nil，则设置为关闭错误
// 如果原错误非 nil，则使用 errors.Join 合并两个错误
//
// 用法:
//
//	func processFile(path string) (err error) {
//	    file, err := os.Open(path)
//	    if err != nil {
//	        return err
//	    }
//	    defer utils.CloseWithError(file, &err)
//	    // ... 处理文件
//	    return nil
//	}
func CloseWithError(closer io.Closer, errp *error) {
	if closer == nil {
		return
	}
	closeErr := closer.Close()
	if closeErr == nil {
		return
	}
	if errp == nil {
		return
	}
	if *errp == nil {
		*errp = closeErr
	} else {
		*errp = errors.Join(*errp, closeErr)
	}
}

// CloserFunc 将函数转换为 io.Closer 接口
//
// 适用于需要在 defer 中调用非标准关闭函数的场景
//
// 用法:
//
//	cleanup := utils.CloserFunc(func() error {
//	    return someResource.Shutdown()
//	})
//	defer utils.CloseQuietly(cleanup)
type CloserFunc func() error

// Close 实现 io.Closer 接口
func (f CloserFunc) Close() error {
	if f == nil {
		return nil
	}
	return f()
}

// MultiCloser 组合多个 Closer，按顺序关闭
//
// 适用于需要同时关闭多个资源的场景
// 所有错误会被收集并合并返回
//
// 用法:
//
//	mc := utils.NewMultiCloser(file1, file2, conn)
//	defer utils.CloseQuietly(mc)
type MultiCloser struct {
	closers []io.Closer
}

// NewMultiCloser 创建 MultiCloser
func NewMultiCloser(closers ...io.Closer) *MultiCloser {
	return &MultiCloser{closers: closers}
}

// Add 添加 Closer
func (mc *MultiCloser) Add(closer io.Closer) {
	if closer != nil {
		mc.closers = append(mc.closers, closer)
	}
}

// Close 关闭所有资源，合并所有错误
func (mc *MultiCloser) Close() error {
	var errs []error
	for _, closer := range mc.closers {
		if closer != nil {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
