package resolver

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"Flux-KV/pkg/network/discovery"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
)

const (
	etcdScheme      = "etcd"
	defaultPrefix   = "/services/kv-service/"
	defaultDialTimeout = 5 * time.Second
)

// EtcdResolverBuilder 实现 grpc resolver.Builder
type EtcdResolverBuilder struct {
	endpoints []string
}

// NewEtcdResolverBuilder 创建 Builder
func NewEtcdResolverBuilder(endpoints []string) *EtcdResolverBuilder {
	return &EtcdResolverBuilder{endpoints: endpoints}
}

// Scheme 返回 resolver 的 scheme
func (b *EtcdResolverBuilder) Scheme() string {
	return etcdScheme
}

// Build 创建 Resolver 实例
func (b *EtcdResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &etcdResolver{
		ctx:       ctx,
		cancel:    cancel,
		cc:        cc,
		endpoints: b.endpoints,
		prefix:    defaultPrefix,
	}
	if err := r.start(); err != nil {
		cancel()
		return nil, err
	}
	return r, nil
}

// etcdResolver 实现 grpc resolver.Resolver
type etcdResolver struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cc        resolver.ClientConn
	endpoints []string
	prefix    string
	client    *clientv3.Client
	mu        sync.Mutex
}

// start 初始化 Etcd 连接并启动监听
func (r *etcdResolver) start() error {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   r.endpoints,
		DialTimeout: defaultDialTimeout,
	})
	if err != nil {
		return err
	}
	r.client = cli

	// 先拉取全量节点列表
	if err := r.resolve(); err != nil {
		return err
	}

	// 启动 Watch 监听变化
	go r.watch()
	return nil
}

// ResolveNow 触发立即解析（被 gRPC 调用）
func (r *etcdResolver) ResolveNow(resolver.ResolveNowOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.resolve(); err != nil {
		log.Printf("[EtcdResolver] resolve failed: %v", err)
	}
}

// Close 关闭 Resolver
func (r *etcdResolver) Close() {
	r.cancel()
	if r.client != nil {
		r.client.Close()
	}
}

// resolve 从 Etcd 拉取当前所有节点并更新 gRPC 地址状态
func (r *etcdResolver) resolve() error {
	resp, err := r.client.Get(r.ctx, r.prefix, clientv3.WithPrefix())
	if err != nil {
		return err
	}

	addrs := make([]resolver.Address, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		addr, err := r.parseAddress(string(kv.Key), string(kv.Value))
		if err != nil {
			log.Printf("[EtcdResolver] parse address failed for key %s: %v", string(kv.Key), err)
			continue
		}
		addrs = append(addrs, addr)
	}

	if err := r.cc.UpdateState(resolver.State{Addresses: addrs}); err != nil {
		return err
	}
	log.Printf("[EtcdResolver] updated %d addresses", len(addrs))
	return nil
}

// watch 监听 Etcd 前缀变化并实时更新地址
func (r *etcdResolver) watch() {
	watchCh := r.client.Watch(r.ctx, r.prefix, clientv3.WithPrefix())
	for {
		select {
		case <-r.ctx.Done():
			return
		case resp, ok := <-watchCh:
			if !ok {
				log.Println("[EtcdResolver] watch channel closed")
				return
			}
			if resp.Err() != nil {
				log.Printf("[EtcdResolver] watch error: %v", resp.Err())
				continue
			}
			// 任何变化都触发全量重新解析（简单可靠）
			r.ResolveNow(resolver.ResolveNowOptions{})
		}
	}
}

// parseAddress 从 Etcd Key-Value 解析为 gRPC resolver.Address
func (r *etcdResolver) parseAddress(key, value string) (resolver.Address, error) {
	// key 格式: /services/kv-service/<addr>
	parts := strings.Split(key, "/")
	addr := parts[len(parts)-1]
	if addr == "" {
		addr = parts[len(parts)-2]
	}

	// value 是 JSON 格式的 NodeInfo
	var info discovery.NodeInfo
	if err := json.Unmarshal([]byte(value), &info); err != nil {
		// 如果解析失败，仍返回地址，只是没有属性
		return resolver.Address{Addr: addr}, nil
	}

	attrs := attributes.New("mode", info.Mode).
		WithValue("is_leader", info.IsLeader).
		WithValue("group_id", info.GroupID).
		WithValue("node_id", info.NodeID)

	return resolver.Address{
		Addr:       addr,
		Attributes: attrs,
	}, nil
}

// Register 注册 Etcd Resolver 到 gRPC
func Register(endpoints []string) {
	resolver.Register(NewEtcdResolverBuilder(endpoints))
}
