package discovery

import (
	"context"
	"log"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Registry 用于把服务注册到 Etcd
type Registry struct {
	cli     *clientv3.Client   // Etcd 的客户端连接
	leaseID clientv3.LeaseID   // 租约 ID
	cancel  context.CancelFunc // 用于停止旧的 keepalive goroutine
	mu      sync.Mutex
}

// NewRegistry 建立连接
func NewRegistry(endpoints []string) (*Registry, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return &Registry{cli: cli}, nil
}

// Register 核心逻辑：注册并自动续约
// key: 你的服务名 (例如 /kv-service/localhost:8080)
// value: 你的服务地址 (例如 localhost:8080)
// ttl: 生存时间 (秒)，比如 5 秒
func (r *Registry) Register(ctx context.Context, key, value string, ttl int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 停止旧的 keepalive goroutine（如果存在）
	if r.cancel != nil {
		r.cancel()
	}

	// 第一步：申请租约
	grantResp, err := r.cli.Grant(ctx, ttl)
	if err != nil {
		return err
	}
	r.leaseID = grantResp.ID

	// 第二步：写入数据，并绑定租约
	_, err = r.cli.Put(ctx, key, value, clientv3.WithLease(r.leaseID))
	if err != nil {
		return err
	}

	// 第三步：开始自动续约
	keepCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	keepAliveCh, err := r.cli.KeepAlive(keepCtx, r.leaseID)
	if err != nil {
		return err
	}

	// 开启一个协程来处理续约响应
	go func() {
		for {
			select {
			case _, ok := <-keepAliveCh:
				if !ok {
					log.Println("[Registry] Etcd keepalive channel closed")
					return
				}
			case <-keepCtx.Done():
				return
			}
		}
	}()

	log.Printf("[Registry] Service registered: Key = %s, LeaseID = %v", key, r.leaseID)
	return nil
}

func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 停止 keepalive goroutine
	if r.cancel != nil {
		r.cancel()
	}

	// 撤销租约，Etcd 会立即删除 Key
	if r.cli != nil {
		r.cli.Revoke(context.Background(), r.leaseID)
		r.cli.Close()
	}
}

// Status checks if the Etcd connection is alive by querying cluster status.
func (r *Registry) Status(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cli == nil {
		return context.Canceled
	}
	_, err := r.cli.Status(ctx, r.cli.Endpoints()[0])
	return err
}