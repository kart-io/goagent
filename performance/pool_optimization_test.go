package performance

import (
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
)

// BenchmarkChainInputPool tests ChainInput object pool performance
func BenchmarkChainInputPool(b *testing.B) {
	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			input := &core.ChainInput{
				Data: "test",
				Vars: make(map[string]interface{}, 8),
				Options: core.ChainOptions{
					StopOnError: true,
					Extra:       make(map[string]interface{}, 4),
				},
			}
			_ = input
		}
	})

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			input := core.GetChainInput()
			input.Data = "test"
			core.PutChainInput(input)
		}
	})
}

// BenchmarkChainOutputPool tests ChainOutput object pool performance
func BenchmarkChainOutputPool(b *testing.B) {
	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			output := &core.ChainOutput{
				Data:          "result",
				StepsExecuted: make([]core.StepExecution, 0, 8),
				Status:        "success",
				Metadata:      make(map[string]interface{}, 4),
			}
			_ = output
		}
	})

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			output := core.GetChainOutput()
			output.Data = "result"
			output.Status = "success"
			core.PutChainOutput(output)
		}
	})
}

// BenchmarkMiddlewareRequestPool tests MiddlewareRequest object pool performance
func BenchmarkMiddlewareRequestPool(b *testing.B) {
	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			req := &middleware.MiddlewareRequest{
				Input:     "test",
				Metadata:  make(map[string]interface{}, 4),
				Headers:   make(map[string]string, 4),
				Timestamp: time.Now(),
			}
			_ = req
		}
	})

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			req := middleware.GetMiddlewareRequest()
			req.Input = "test"
			req.Timestamp = time.Now()
			middleware.PutMiddlewareRequest(req)
		}
	})
}

// BenchmarkMiddlewareResponsePool tests MiddlewareResponse object pool performance
func BenchmarkMiddlewareResponsePool(b *testing.B) {
	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			resp := &middleware.MiddlewareResponse{
				Output:   "result",
				Metadata: make(map[string]interface{}, 4),
				Headers:  make(map[string]string, 4),
				Duration: time.Second,
			}
			_ = resp
		}
	})

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			resp := middleware.GetMiddlewareResponse()
			resp.Output = "result"
			resp.Duration = time.Second
			middleware.PutMiddlewareResponse(resp)
		}
	})
}

// BenchmarkPoolConcurrentAccess tests concurrent access to object pools
func BenchmarkPoolConcurrentAccess(b *testing.B) {
	b.Run("ChainInput", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				input := core.GetChainInput()
				input.Data = "test"
				core.PutChainInput(input)
			}
		})
	})

	b.Run("ChainOutput", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				output := core.GetChainOutput()
				output.Data = "result"
				core.PutChainOutput(output)
			}
		})
	})

	b.Run("MiddlewareRequest", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req := middleware.GetMiddlewareRequest()
				req.Input = "test"
				middleware.PutMiddlewareRequest(req)
			}
		})
	})

	b.Run("MiddlewareResponse", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp := middleware.GetMiddlewareResponse()
				resp.Output = "result"
				middleware.PutMiddlewareResponse(resp)
			}
		})
	})
}

// BenchmarkPoolWithData tests pool performance with realistic data
func BenchmarkPoolWithData(b *testing.B) {
	b.Run("ChainInputWithData", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			input := core.GetChainInput()
			input.Data = "test data"
			input.Vars["key1"] = "value1"
			input.Vars["key2"] = "value2"
			input.Options.Timeout = 30 * time.Second
			core.PutChainInput(input)
		}
	})

	b.Run("ChainOutputWithData", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			output := core.GetChainOutput()
			output.Data = "result data"
			output.Status = "success"
			output.StepsExecuted = append(output.StepsExecuted, core.StepExecution{
				StepNumber: 1,
				StepName:   "step1",
				Success:    true,
			})
			output.Metadata["timing"] = time.Now()
			core.PutChainOutput(output)
		}
	})

	b.Run("MiddlewareRequestWithData", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			req := middleware.GetMiddlewareRequest()
			req.Input = "test input"
			req.Metadata["trace_id"] = "12345"
			req.Headers["Authorization"] = "Bearer token"
			req.Timestamp = time.Now()
			middleware.PutMiddlewareRequest(req)
		}
	})

	b.Run("MiddlewareResponseWithData", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			resp := middleware.GetMiddlewareResponse()
			resp.Output = "result output"
			resp.Metadata["latency"] = 100 * time.Millisecond
			resp.Headers["Content-Type"] = "application/json"
			resp.Duration = time.Second
			middleware.PutMiddlewareResponse(resp)
		}
	})
}

// BenchmarkPoolReuse tests pool object reuse efficiency
func BenchmarkPoolReuse(b *testing.B) {
	b.Run("ChainInputReuse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Acquire
			input := core.GetChainInput()
			input.Data = "test"
			input.Vars["key"] = "value"

			// Use it
			_ = input.Data
			_ = input.Vars["key"]

			// Return
			core.PutChainInput(input)
		}
	})

	b.Run("MiddlewareRequestReuse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Acquire
			req := middleware.GetMiddlewareRequest()
			req.Input = "test"
			req.Metadata["key"] = "value"

			// Use it
			_ = req.Input
			_ = req.Metadata["key"]

			// Return
			middleware.PutMiddlewareRequest(req)
		}
	})
}
