package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaxItemsZeroIsUnlimited(t *testing.T) {
	const n = 2000
	count := func(limits Limits[int]) (int, error) {
		r, err := AsRunnable(func(context.Context, int) (Source[int], error) {
			return func(_ context.Context, yield func(int) error) error {
				for range n {
					if err := yield(1); err != nil {
						return err
					}
				}
				return nil
			}, nil
		}, sumFold(), nil, limits)
		require.NoError(t, err)
		return r.Invoke(context.Background(), 0)
	}
	got, err := count(Limits[int]{})
	require.NoError(t, err, "旧默认 1024 条会报错")
	require.Equal(t, n, got)
	_, err = count(Limits[int]{MaxItems: 10})
	require.Error(t, err)
}
