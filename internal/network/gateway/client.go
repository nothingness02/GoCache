package gateway

import "context"

// KVClient 定义 Gateway 与后端 KV 服务交互的接口
// 内部 gRPC Client（pkg/network/client/）和 Gateway gRPC adapter 共用此接口
type KVClient interface {
	SetWithMode(ctx context.Context, key, value string, mode string) error
	GetWithMode(ctx context.Context, key string, mode string) (string, error)
	DelWithMode(ctx context.Context, key string, mode string) error
}
