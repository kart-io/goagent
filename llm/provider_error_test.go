package llm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderErrorRetryable(t *testing.T) {
	for _, tc := range []struct {
		err  *ProviderError
		want bool
	}{
		{&ProviderError{StatusCode: 429, Err: errors.New("x")}, true},
		{&ProviderError{StatusCode: 503, Err: errors.New("x")}, true},
		{&ProviderError{StatusCode: 400, Err: errors.New("x")}, false},
		{&ProviderError{StatusCode: 401, Err: errors.New("x")}, false},
		{&ProviderError{StatusCode: 404, Err: errors.New("x")}, false},
		{&ProviderError{Err: urlErr(&net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)})}, true},
		{&ProviderError{Err: urlErr(&net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)})}, true},
		{&ProviderError{Err: urlErr(timeoutError{})}, true},
		{&ProviderError{Err: urlErr(&net.DNSError{Err: "server misbehaving", Name: "api.example.com", IsTemporary: true})}, true},
		{&ProviderError{Err: urlErr(io.EOF)}, true},
		{&ProviderError{Err: fmt.Errorf("read: %w", io.ErrUnexpectedEOF)}, true},
		// *url.Error 总是实现 net.Error，但证书、协议与域名不存在的错误重试也不会消失。
		{&ProviderError{Err: urlErr(&net.DNSError{Err: "no such host", Name: "api.example.com", IsNotFound: true})}, false},
		{&ProviderError{Err: urlErr(x509.UnknownAuthorityError{})}, false},
		{&ProviderError{Err: urlErr(&tls.CertificateVerificationError{Err: x509.HostnameError{Host: "x"}})}, false},
		{&ProviderError{Err: urlErr(tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"})}, false},
		{&ProviderError{Err: urlErr(errors.New(`unsupported protocol scheme "ftp"`))}, false},
		{&ProviderError{Err: context.Canceled}, false},
		{&ProviderError{Err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded)}, false},
		{&ProviderError{Err: errors.New("decode failure")}, false},
	} {
		require.Equal(t, tc.want, tc.err.Retryable(), tc.err.Err.Error())
	}
	e := &ProviderError{StatusCode: 500, Err: errors.New("server")}
	require.Equal(t, "server", e.Error())
	var target *ProviderError
	require.True(t, errors.As(fmt.Errorf("outer: %w", e), &target))
}

func urlErr(err error) error {
	return &url.Error{Op: "Post", URL: "https://api.example.com/v1", Err: err}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestClassifyError(t *testing.T) {
	cases := []struct {
		err  error
		want ClassifiedError
	}{
		{&ProviderError{StatusCode: 429, Err: errors.New("slow down")}, ClassifiedError{Reason: ReasonRateLimit, Retryable: true, ShouldFallback: true}},
		{&ProviderError{StatusCode: 503, Err: errors.New("unavailable")}, ClassifiedError{Reason: ReasonServer, Retryable: true, ShouldFallback: true}},
		{&ProviderError{StatusCode: 401, Err: errors.New("bad key")}, ClassifiedError{Reason: ReasonAuth, ShouldFallback: true}},
		{&ProviderError{StatusCode: 404, Err: errors.New("no such model")}, ClassifiedError{Reason: ReasonNotFound, ShouldFallback: true}},
		{&ProviderError{StatusCode: 400, Err: errors.New("This model's maximum context length is 128000 tokens")}, ClassifiedError{Reason: ReasonContextLength, ShouldCompress: true}},
		{&ProviderError{StatusCode: 413, Err: errors.New("payload too large")}, ClassifiedError{Reason: ReasonContextLength, ShouldCompress: true}},
		{&ProviderError{StatusCode: 400, Err: errors.New("invalid tool schema")}, ClassifiedError{Reason: ReasonBadRequest}},
		{&ProviderError{Err: io.ErrUnexpectedEOF}, ClassifiedError{Reason: ReasonNetwork, Retryable: true}},
		{fmt.Errorf("wrapped: %w", context.Canceled), ClassifiedError{Reason: ReasonCanceled}},
		{errors.New("prompt is too long: 250000 tokens"), ClassifiedError{Reason: ReasonContextLength, ShouldCompress: true}},
		{errors.New("something odd"), ClassifiedError{Reason: ReasonUnknown}},
	}
	for _, c := range cases {
		if got := ClassifyError(c.err); got != c.want {
			t.Errorf("ClassifyError(%v) = %+v, want %+v", c.err, got, c.want)
		}
	}
}
