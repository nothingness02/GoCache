package interceptors

import (
	"context"
	"math/rand"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryRetryInterceptor 返回一个 gRPC unary 客户端拦截器，对 Unavailable / DeadlineExceeded 错误进行指数退避重试
func UnaryRetryInterceptor(maxRetries int, baseDelay, maxDelay time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		var lastErr error
		delay := baseDelay

		for attempt := 0; attempt <= maxRetries; attempt++ {
			err := invoker(ctx, method, req, reply, cc, opts...)
			if err == nil {
				return nil
			}
			lastErr = err

			if attempt == maxRetries {
				break
			}

			st, ok := status.FromError(err)
			if !ok || (st.Code() != codes.Unavailable && st.Code() != codes.DeadlineExceeded) {
				return err
			}

			sleep := delay
			if int64(delay) > 0 {
				sleep = delay + time.Duration(rand.Int63n(int64(delay)/2))
			}

			timer := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			timer.Stop()

			delay = time.Duration(float64(delay) * 2)
			if maxDelay > 0 && delay > maxDelay {
				delay = maxDelay
			}
		}
		return lastErr
	}
}
