package core

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratorFunc(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 5; i++ {
			if !yield(i, nil) {
				return
			}
		}
	})

	var results []int
	for val, err := range gen {
		require.NoError(t, err)
		results = append(results, val)
	}

	assert.Equal(t, []int{0, 1, 2, 3, 4}, results)
}

func TestToChannel(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 3; i++ {
			yield(i, nil)
		}
	})

	ctx := context.Background()
	ch := ToChannel(ctx, gen, 0)

	var results []int
	for event := range ch {
		require.NoError(t, event.Error)
		results = append(results, event.Data)
	}

	assert.Equal(t, []int{0, 1, 2}, results)
}

func TestToChannel_WithContext(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 10; i++ {
			yield(i, nil)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := ToChannel(ctx, gen, 0)

	// 读取前 2 个值后取消
	results := []int{}
	count := 0
	for event := range ch {
		if count == 2 {
			cancel()
		}
		if event.Error != nil {
			assert.Equal(t, context.Canceled, event.Error)
			break
		}
		results = append(results, event.Data)
		count++
	}

	// 应该至少有 2 个结果
	assert.GreaterOrEqual(t, len(results), 2)
}

func TestFromChannel(t *testing.T) {
	ch := make(chan StreamChunk[int], 3)
	ch <- StreamChunk[int]{Data: 1}
	ch <- StreamChunk[int]{Data: 2}
	ch <- StreamChunk[int]{Data: 3}
	close(ch)

	gen := FromChannel(ch)

	var results []int
	for val, err := range gen {
		require.NoError(t, err)
		results = append(results, val)
	}

	assert.Equal(t, []int{1, 2, 3}, results)
}

func TestFromChannel_WithError(t *testing.T) {
	testErr := errors.New("test error")
	ch := make(chan StreamChunk[int], 3)
	ch <- StreamChunk[int]{Data: 1}
	ch <- StreamChunk[int]{Data: 2, Error: testErr}
	ch <- StreamChunk[int]{Data: 3} // 不应该读到这个
	close(ch)

	gen := FromChannel(ch)

	var results []int
	var gotErr error
	for val, err := range gen {
		if err != nil {
			gotErr = err
			break
		}
		results = append(results, val)
	}

	assert.Equal(t, []int{1}, results)
	assert.Equal(t, testErr, gotErr)
}

func TestCollect(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 5; i++ {
			yield(i, nil)
		}
	})

	results, err := Collect(gen)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, results)
}

func TestCollect_WithError(t *testing.T) {
	testErr := errors.New("test error")
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		yield(0, nil)
		yield(1, nil)
		if !yield(0, testErr) { // 错误
			return
		}
		yield(2, nil) // 不应该执行到这里
	})

	results, err := Collect(gen)
	assert.Error(t, err)
	assert.Equal(t, testErr, err)
	assert.Equal(t, []int{0, 1}, results) // 错误前的结果
}

func TestTake(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 10; i++ {
			yield(i, nil)
		}
	})

	taken := Take(gen, 3)

	var results []int
	for val, err := range taken {
		require.NoError(t, err)
		results = append(results, val)
	}

	assert.Equal(t, []int{0, 1, 2}, results)
}

func TestTake_LessThanAvailable(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 2; i++ {
			yield(i, nil)
		}
	})

	taken := Take(gen, 5)

	var results []int
	for val, err := range taken {
		require.NoError(t, err)
		results = append(results, val)
	}

	assert.Equal(t, []int{0, 1}, results)
}

func TestFilter(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 10; i++ {
			yield(i, nil)
		}
	})

	// 仅保留偶数
	filtered := Filter(gen, func(val int) bool {
		return val%2 == 0
	})

	var results []int
	for val, err := range filtered {
		require.NoError(t, err)
		results = append(results, val)
	}

	assert.Equal(t, []int{0, 2, 4, 6, 8}, results)
}

func TestFilter_WithError(t *testing.T) {
	testErr := errors.New("test error")
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		yield(1, nil)
		yield(2, nil)
		if !yield(0, testErr) {
			return
		}
	})

	filtered := Filter(gen, func(val int) bool {
		return val%2 == 0
	})

	var results []int
	var gotErr error
	for val, err := range filtered {
		if err != nil {
			gotErr = err
			break
		}
		results = append(results, val)
	}

	assert.Equal(t, []int{2}, results)
	assert.Equal(t, testErr, gotErr)
}

func TestMap(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 5; i++ {
			yield(i, nil)
		}
	})

	// 映射为字符串
	mapped := Map(gen, func(val int) string {
		return fmt.Sprintf("num_%d", val)
	})

	var results []string
	for val, err := range mapped {
		require.NoError(t, err)
		results = append(results, val)
	}

	expected := []string{"num_0", "num_1", "num_2", "num_3", "num_4"}
	assert.Equal(t, expected, results)
}

func TestMap_WithError(t *testing.T) {
	testErr := errors.New("test error")
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		yield(1, nil)
		yield(2, nil)
		if !yield(0, testErr) {
			return
		}
	})

	mapped := Map(gen, func(val int) string {
		return fmt.Sprintf("num_%d", val)
	})

	var results []string
	var gotErr error
	for val, err := range mapped {
		if err != nil {
			gotErr = err
			break
		}
		results = append(results, val)
	}

	assert.Equal(t, []string{"num_1", "num_2"}, results)
	assert.Equal(t, testErr, gotErr)
}

func TestGenerator_EarlyTermination(t *testing.T) {
	callCount := 0
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 10; i++ {
			callCount++
			if !yield(i, nil) {
				return // 早期终止
			}
		}
	})

	// 仅迭代 3 次
	count := 0
	for range gen {
		count++
		if count >= 3 {
			break
		}
	}

	assert.Equal(t, 3, count)
	assert.Equal(t, 3, callCount) // 验证早期终止生效
}

func TestGenerator_ChainedOperations(t *testing.T) {
	// 测试链式操作：生成 -> 过滤 -> 映射 -> 取前3个
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		for i := 0; i < 10; i++ {
			yield(i, nil)
		}
	})

	// 链式操作
	result := Take(
		Map(
			Filter(gen, func(val int) bool {
				return val%2 == 0 // 只要偶数
			}),
			func(val int) string {
				return fmt.Sprintf("even_%d", val)
			},
		),
		3,
	)

	var results []string
	for val, err := range result {
		require.NoError(t, err)
		results = append(results, val)
	}

	expected := []string{"even_0", "even_2", "even_4"}
	assert.Equal(t, expected, results)
}

func TestGenerator_EmptyGenerator(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		// 不产生任何值
	})

	results, err := Collect(gen)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestGenerator_SingleValue(t *testing.T) {
	gen := GeneratorFunc(func(yield func(int, error) bool) {
		yield(42, nil)
	})

	results, err := Collect(gen)
	require.NoError(t, err)
	assert.Equal(t, []int{42}, results)
}
