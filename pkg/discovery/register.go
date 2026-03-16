package discovery

import (
	"context"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Registry 用于把服务注册到 Etcd
type Registry struct {
	cli *clientv3.Client		// Etcd 的客户端连接
	leaseID clientv3.LeaseID	// 租约 ID
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
	// 第一步：申请租约
	// 告诉 Etcd：给我一个 5秒 的有效期
	grantResp, err := r.cli.Grant(ctx, ttl)
	if err != nil {
		return err
	}
	r.leaseID = grantResp.ID

	// 第二步：写入数据，并绑定租约
	// 告诉 Etcd：存入这个 Key-Value，如果租约过期了，这个 Key 也自动删掉
	_, err = r.cli.Put(ctx, key, value, clientv3.WithLease(r.leaseID))
	if err != nil {
		return err
	}

	// 第三步：开始自动续约
	// 这是一个长连接，Client 会一直在后台发心跳
	keepAliveCh, err := r.cli.KeepAlive(ctx, r.leaseID)
	if err != nil {
		return err
	}

	// 开启一个协程来处理续约的相应
	go func() {
		for {
			select {
			case _, ok := <-keepAliveCh:
				// 若通道关闭，则续约断了
				if !ok {
					log.Println("Etcd 续约通道已关闭，服务可能与 Etcd 断连")
					return
				}
			}
		}
	}()

	log.Printf("服务注册成功：Key = %s, ID = %v", key, r.leaseID)
	return nil
}

func (r *Registry) Close() {
	// 撤销租约， Etcd 会立即删除 Key
	if r.cli != nil {
		r.cli.Revoke(context.Background(), r.leaseID)
		r.cli.Close()
	}
}