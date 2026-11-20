package management

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestLimitsFieldsIndependent(t *testing.T) {
	got, err := Limits{Workers: 1}.normalize()
	require.NoError(t, err)
	require.Equal(t, Limits{
		PageSize: DefaultPageSize, Workers: 1,
		PollInterval: DefaultPollInterval, OutboxPollInterval: DefaultOutboxPollInterval, PersistTimeout: DefaultPersistTimeout,
		DeliveryBackoffMax: DefaultDeliveryBackoffMax, BuildRetryDelay: DefaultBuildRetryDelay,
	}, got, "caps stay 0 (unlimited), required values take their defaults")

	got, err = Limits{MaxPageSize: 20}.normalize()
	require.NoError(t, err)
	require.Equal(t, 20, got.PageSize, "default page size never exceeds MaxPageSize")
	_, err = Limits{PageSize: 30, MaxPageSize: 20}.normalize()
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)

	got, err = Limits{PageSize: 500}.normalize()
	require.NoError(t, err)
	require.Equal(t, 500, got.PageSize, "no fixed 200 page ceiling")

	keep, err := DefaultLimits().normalize()
	require.NoError(t, err)
	require.Equal(t, DefaultLimits(), keep)
}

func TestRequiredParamsRejectNegativeAndDefaultOnZero(t *testing.T) {
	tests := []struct {
		name  string
		set   func(*Limits, int)
		read  func(Limits) int64
		value int64
	}{
		{"Workers", func(l *Limits, n int) { l.Workers = n }, func(l Limits) int64 { return int64(l.Workers) }, DefaultWorkers},
		{"PollInterval", func(l *Limits, n int) { l.PollInterval = time.Duration(n) }, func(l Limits) int64 { return int64(l.PollInterval) }, int64(DefaultPollInterval)},
		{"OutboxPollInterval", func(l *Limits, n int) { l.OutboxPollInterval = time.Duration(n) }, func(l Limits) int64 { return int64(l.OutboxPollInterval) }, int64(DefaultOutboxPollInterval)},
		{"PersistTimeout", func(l *Limits, n int) { l.PersistTimeout = time.Duration(n) }, func(l Limits) int64 { return int64(l.PersistTimeout) }, int64(DefaultPersistTimeout)},
		{"DeliveryBackoffMax", func(l *Limits, n int) { l.DeliveryBackoffMax = time.Duration(n) }, func(l Limits) int64 { return int64(l.DeliveryBackoffMax) }, int64(DefaultDeliveryBackoffMax)},
		{"BuildRetryDelay", func(l *Limits, n int) { l.BuildRetryDelay = time.Duration(n) }, func(l Limits) int64 { return int64(l.BuildRetryDelay) }, int64(DefaultBuildRetryDelay)},
		{"PageSize", func(l *Limits, n int) { l.PageSize = n }, func(l Limits) int64 { return int64(l.PageSize) }, DefaultPageSize},
		{"DeliveryTimeout", func(l *Limits, n int) { l.DeliveryTimeout = time.Duration(n) }, func(l Limits) int64 { return int64(l.DeliveryTimeout) }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l Limits
			tt.set(&l, -1)
			_, err := l.normalize()
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
			l = Limits{}
			got, err := l.normalize()
			require.NoError(t, err)
			require.Equal(t, tt.value, tt.read(got))
			tt.set(&l, 7)
			got, err = l.normalize()
			require.NoError(t, err)
			require.Equal(t, int64(7), tt.read(got))
		})
	}
}

func TestRedeliveryDelayHonoursCeiling(t *testing.T) {
	require.Equal(t, 2*time.Second, redeliveryDelay(1, DefaultDeliveryBackoffMax))
	require.Equal(t, 256*time.Second, redeliveryDelay(8, DefaultDeliveryBackoffMax))
	require.Equal(t, 256*time.Second, redeliveryDelay(50, DefaultDeliveryBackoffMax))
	require.Equal(t, 5*time.Second, redeliveryDelay(50, 5*time.Second))
}

func TestRequestBytesAntiAbuseCap(t *testing.T) {
	for _, tc := range []struct {
		in, want int64
		invalid  bool
	}{
		{0, DefaultMaxRequestBytes, false},
		{64, 64, false},
		{8 << 20, 8 << 20, false},
		{UnlimitedRequestBytes, UnlimitedRequestBytes, false},
		{-2, 0, true},
	} {
		got, err := RequestBytes(tc.in)
		if tc.invalid {
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}
