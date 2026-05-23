package client

import (
	"context"
	"time"

	pb "Flux-KV/api/proto"
	"Flux-KV/pkg/network/client/balancer"
	"Flux-KV/pkg/network/client/interceptors"
	"Flux-KV/pkg/network/client/resolver"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// ConsistencyMode 一致性模式
type ConsistencyMode string

const (
	ModeAP ConsistencyMode = "ap"
	ModeCP ConsistencyMode = "cp"
)

// Client 是 gRPC Client SDK，支持两种模式：
//   1. Etcd 服务发现模式（Gateway 内部使用，自动发现后端 Server）
//   2. 直连模式（外部程序使用，直接连接 Gateway 或指定 Server）
type Client struct {
	conn *grpc.ClientConn
}

// NewGRPCConn 创建基于 Etcd Resolver + Flux Balancer 的 gRPC 连接
// 适用于 Gateway 内部连接后端 Server 节点
func NewGRPCConn(etcdEndpoints []string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	resolver.Register(etcdEndpoints)
	balancer.Register()

	defaultOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"flux": {}}]}`),
		grpc.WithUnaryInterceptor(interceptors.UnaryRetryInterceptor(3, 100*time.Millisecond, 2*time.Second)),
	}

	allOpts := append(defaultOpts, opts...)
	return grpc.NewClient("etcd:///services/kv-service/", allOpts...)
}

// NewDirectConn 创建直接连接指定地址的 gRPC 连接
// 适用于外部程序直接连接 Gateway
func NewDirectConn(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	defaultOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.WithUnaryInterceptor(interceptors.UnaryRetryInterceptor(3, 100*time.Millisecond, 2*time.Second)),
	}

	allOpts := append(defaultOpts, opts...)
	return grpc.NewClient(target, allOpts...)
}

// NewClient 从已有 gRPC 连接创建 Client
func NewClient(conn *grpc.ClientConn) *Client {
	return &Client{conn: conn}
}

// Close 关闭底层 gRPC 连接
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SetWithMode 支持指定一致性模式的 Set（mode 为 "ap" 或 "cp"）
func (c *Client) SetWithMode(ctx context.Context, key, value string, mode string) error {
	ctx = metadata.AppendToOutgoingContext(ctx, "x-flux-mode", mode, "x-flux-key", key)
	_, err := pb.NewKVServiceClient(c.conn).Set(ctx, &pb.SetRequest{Key: key, Value: value})
	return err
}

// GetWithMode 支持指定一致性模式的 Get（mode 为 "ap" 或 "cp"）
// AP 模式下若 active ring 未命中，会自动回退到 prev ring 尝试读取（扩容兼容）
func (c *Client) GetWithMode(ctx context.Context, key string, mode string) (string, error) {
	ctx = metadata.AppendToOutgoingContext(ctx, "x-flux-mode", mode, "x-flux-key", key)
	cli := pb.NewKVServiceClient(c.conn)
	resp, err := cli.Get(ctx, &pb.GetRequest{Key: key})
	if err != nil {
		return "", err
	}
	if resp.Found {
		return resp.Value, nil
	}
	// AP 模式未命中时，尝试旧环回退
	if mode == "ap" {
		ctx2 := metadata.AppendToOutgoingContext(ctx, "x-flux-ring", "prev")
		resp2, err2 := cli.Get(ctx2, &pb.GetRequest{Key: key})
		if err2 == nil && resp2.Found {
			return resp2.Value, nil
		}
	}
	return resp.Value, nil
}

// DelWithMode 支持指定一致性模式的 Del（mode 为 "ap" 或 "cp"）
func (c *Client) DelWithMode(ctx context.Context, key string, mode string) error {
	ctx = metadata.AppendToOutgoingContext(ctx, "x-flux-mode", mode, "x-flux-key", key)
	_, err := pb.NewKVServiceClient(c.conn).Del(ctx, &pb.DelRequest{Key: key})
	return err
}

// InternalSet 节点间数据迁移专用写入（直接发送到目标节点，绕过一致性哈希）
func (c *Client) InternalSet(ctx context.Context, key, value string) error {
	_, err := pb.NewKVServiceClient(c.conn).InternalSet(ctx, &pb.InternalSetRequest{Key: key, Value: value})
	return err
}
