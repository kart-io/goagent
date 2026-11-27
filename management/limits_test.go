package management

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestLimitsFieldsIndependent(t *testing.T) {
	got, err := Limits{Workers: 1}.normalize()
	require.NoError(t, err)
	require.Equal(t, Limits{
		PageSize: constants.DefaultManagementPageSize, Workers: 1,
		PollInterval: constants.DefaultManagementPollInterval, OutboxPollInterval: constants.DefaultManagementOutboxPollInterval, PersistTimeout: constants.DefaultManagementPersistTimeout,
		DeliveryBackoffMax: constants.DefaultManagementDeliveryBackoffMax, BuildRetryDelay: constants.DefaultManagementBuildRetryDelay,
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
		{"Workers", func(l *Limits, n int) { l.Workers = n }, func(l Limits) int64 { return int64(l.Workers) }, constants.DefaultManagementWorkers},
		{"PollInterval", func(l *Limits, n int) { l.PollInterval = time.Duration(n) }, func(l Limits) int64 { return int64(l.PollInterval) }, int64(constants.DefaultManagementPollInterval)},
		{"OutboxPollInterval", func(l *Limits, n int) { l.OutboxPollInterval = time.Duration(n) }, func(l Limits) int64 { return int64(l.OutboxPollInterval) }, int64(constants.DefaultManagementOutboxPollInterval)},
		{"PersistTimeout", func(l *Limits, n int) { l.PersistTimeout = time.Duration(n) }, func(l Limits) int64 { return int64(l.PersistTimeout) }, int64(constants.DefaultManagementPersistTimeout)},
		{"DeliveryBackoffMax", func(l *Limits, n int) { l.DeliveryBackoffMax = time.Duration(n) }, func(l Limits) int64 { return int64(l.DeliveryBackoffMax) }, int64(constants.DefaultManagementDeliveryBackoffMax)},
		{"BuildRetryDelay", func(l *Limits, n int) { l.BuildRetryDelay = time.Duration(n) }, func(l Limits) int64 { return int64(l.BuildRetryDelay) }, int64(constants.DefaultManagementBuildRetryDelay)},
		{"PageSize", func(l *Limits, n int) { l.PageSize = n }, func(l Limits) int64 { return int64(l.PageSize) }, constants.DefaultManagementPageSize},
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
	require.Equal(t, 2*time.Second, redeliveryDelay(1, constants.DefaultManagementDeliveryBackoffMax))
	require.Equal(t, 256*time.Second, redeliveryDelay(8, constants.DefaultManagementDeliveryBackoffMax))
	require.Equal(t, 256*time.Second, redeliveryDelay(50, constants.DefaultManagementDeliveryBackoffMax))
	require.Equal(t, 5*time.Second, redeliveryDelay(50, 5*time.Second))
}

func TestRequestBytesAntiAbuseCap(t *testing.T) {
	for _, tc := range []struct {
		in, want int64
		invalid  bool
	}{
		{0, constants.DefaultManagementMaxRequestBytes, false},
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

func TestSlotLimitsValidated(t *testing.T) {
	for _, tc := range []struct {
		name string
		l    Limits
	}{
		{"负的 ActorWorkers", Limits{ActorWorkers: -1}},
		{"负的 KindWorkers", Limits{KindWorkers: map[string]int{"text": -1}}},
		{"空 RunnerKind", Limits{KindWorkers: map[string]int{"": 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.l.normalize()
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
		})
	}
	kinds := map[string]int{"text": 2}
	got, err := Limits{KindWorkers: kinds, ActorWorkers: 3}.normalize()
	require.NoError(t, err)
	kinds["text"] = 9
	require.Equal(t, 2, got.KindWorkers["text"], "规范化后的配置不随调用方的 map 变化")
	require.Equal(t, 3, got.ActorWorkers)
}
