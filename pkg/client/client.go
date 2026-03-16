package client

import (
	pb "Flux-KV/api/proto"
	"Flux-KV/pkg/consistent"
	"Flux-KV/pkg/discovery"
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// ===== 负载均衡器接口 =====
type LoadBalancer interface {
	// Select 根据 key 选择一个节点客户端（key 用于一致性哈希，nil 表示轮询）
	Select(clients map[string]pb.KVServiceClient, addrs []string, seq *uint64, key string) (pb.KVServiceClient, string, error)
	Name() string
}

// ===== 轮询策略 =====
type RoundRobin struct{}

func (r *RoundRobin) Select(clients map[string]pb.KVServiceClient, addrs []string, seq *uint64, key string) (pb.KVServiceClient, string, error) {
	if len(addrs) == 0 {
		return nil, "", errors.New("no available kv-service nodes")
	}
	// 核心：原子递增，实现 Round-Robin
	next := atomic.AddUint64(seq, 1)
	index := next % uint64(len(addrs))
	addr := addrs[index]
	return clients[addr], addr, nil
}

func (r *RoundRobin) Name() string {
	return "round-robin"
}

// ===== 一致性哈希策略 =====
type ConsistentHash struct {
	ring *consistent.Map
	mu   sync.RWMutex
}

func NewConsistentHash(replicas int) *ConsistentHash {
	return &ConsistentHash{
		ring: consistent.New(replicas, nil),
	}
}

func (c *ConsistentHash) Select(clients map[string]pb.KVServiceClient, addrs []string, seq *uint64, key string) (pb.KVServiceClient, string, error) {
	if len(addrs) == 0 {
		return nil, "", errors.New("no available kv-service nodes")
	}
	if key == "" {
		return nil, "", errors.New("consistent hash requires a key")
	}

	c.mu.RLock()
	addr := c.ring.Get(key)
	c.mu.RUnlock()

	if addr == "" {
		// 环为空或 key 不在范围内，回退到第一个节点
		addr = addrs[0]
	}

	client, ok := clients[addr]
	if !ok {
		// 节点已下线，回退到第一个节点
		addr = addrs[0]
		client = clients[addr]
	}

	return client, addr, nil
}

func (c *ConsistentHash) Name() string {
	return "consistent-hash"
}

// ===== 负载均衡器选项 =====

// LBType 负载均衡类型
type LBType int

const (
	LBTypeRoundRobin LBType = iota
	LBTypeConsistentHash
)

// Option 配置选项
type Option func(*Client)

// WithLoadBalancer 设置负载均衡策略
func WithLoadBalancer(lbType LBType, replicas int) Option {
	return func(c *Client) {
		switch lbType {
		case LBTypeConsistentHash:
			c.lb = NewConsistentHash(replicas)
		default:
			c.lb = &RoundRobin{}
		}
	}
}

// ===== 连接池配置 =====

// PoolConfig 连接池配置
type PoolConfig struct {
	MaxConnsPerHost  int           // 每个主机最大连接数 (0 = 无限制)
	MaxIdleConns     int           // 最大空闲连接数
	IdleConnTimeout  time.Duration // 空闲连接超时
	KeepAlive        time.Duration // 保持连接时间
	KeepAliveTimeout time.Duration // 保持连接超时
	HandshakeTimeout time.Duration // 握手超时
}

// DefaultPoolConfig 默认连接池配置
var DefaultPoolConfig = PoolConfig{
	MaxConnsPerHost:  100, // 每个节点最多 100 个连接
	MaxIdleConns:     10,  // 最多 10 个空闲连接
	IdleConnTimeout:  30 * time.Second,
	KeepAlive:        30 * time.Second,
	KeepAliveTimeout: 10 * time.Second,
	HandshakeTimeout: 10 * time.Second,
}

// WithPoolConfig 设置连接池配置
func WithPoolConfig(cfg PoolConfig) Option {
	return func(c *Client) {
		c.poolCfg = cfg
	}
}

// WithMaxConnsPerHost 设置每个主机的最大连接数
func WithMaxConnsPerHost(maxConns int) Option {
	return func(c *Client) {
		c.poolCfg.MaxConnsPerHost = maxConns
	}
}

// ===== Client 结构体 =====
type Client struct {
	mu      sync.RWMutex
	conns   map[string]*grpc.ClientConn   // addr -> 原始连接
	clients map[string]pb.KVServiceClient // addr -> 客户端存根
	addrs   []string                      // 节点地址列表

	seq     uint64       // 轮询计数器
	lb      LoadBalancer // 负载均衡器
	poolCfg PoolConfig   // 连接池配置
}

// NewClient 初始化客户端管理器，并开始监听服务节点变化
func NewClient(d *discovery.Discovery, serviceName string, opts ...Option) (*Client, error) {
	c := &Client{
		clients: make(map[string]pb.KVServiceClient),
		conns:   make(map[string]*grpc.ClientConn),
		addrs:   make([]string, 0),
		lb:      &RoundRobin{},     // 默认使用轮询
		poolCfg: DefaultPoolConfig, // 默认连接池配置
	}

	// 应用选项
	for _, opt := range opts {
		opt(c)
	}

	log.Printf("📊 [Client] 使用负载均衡策略: %s", c.lb.Name())

	// 启动监听 (回调函数会自动处理现有节点和未来节点的连接建立)
	// 假设 Etcd 中注册的 Key 是 /services/kv-service/uuid
	// 我们监听的前缀就是 /services/kv-service/
	prefix := "/services/" + serviceName + "/"
	err := d.WatchService(prefix, c.addNode, c.removeNode)
	if err != nil {
		return nil, err
	}

	return c, nil
}

// NewDirectClient 创建直连单个节点的客户端（不使用服务发现）
// 适用于测试用例或手动路由场景
func NewDirectClient(addr string, opts ...Option) (*Client, error) {
	c := &Client{
		clients: make(map[string]pb.KVServiceClient),
		conns:   make(map[string]*grpc.ClientConn),
		addrs:   make([]string, 0),
		lb:      &RoundRobin{},
	}

	// 应用选项
	for _, opt := range opts {
		opt(c)
	}

	// 直接添加节点
	c.addNode("direct", addr)

	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.clients) == 0 {
		return nil, errors.New("failed to connect to " + addr)
	}

	return c, nil
}

// Close 关闭底层连接
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for addr, conn := range c.conns {
		conn.Close()
		log.Printf("🔌 [Client] 关闭连接: %s", addr)
	}
	return nil
}

// addNode 节点上线回调：建立连接并加入池子
func (c *Client) addNode(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr := value // Etcd value 存储的是 "ip:port"

	// 防止重复添加
	if _, ok := c.clients[addr]; ok {
		return
	}

	// 建立 gRPC 连接 (应用连接池配置)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		// Keepalive 配置：保持连接活跃
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    c.poolCfg.KeepAlive,
			Timeout: c.poolCfg.KeepAliveTimeout,
		}),
	)
	if err != nil {
		log.Printf("❌ [Client] 连接节点失败 %s: %v", addr, err)
		return
	}

	// 创建存根
	rpcClient := pb.NewKVServiceClient(conn)

	c.clients[addr] = rpcClient
	c.conns[addr] = conn
	c.addrs = append(c.addrs, addr)

	// 如果使用一致性哈希，需要更新哈希环
	if ch, ok := c.lb.(*ConsistentHash); ok {
		ch.mu.Lock()
		ch.ring.Add(addr)
		ch.mu.Unlock()
		log.Printf("🔄 [Client] 一致性哈希环更新: 添加节点 %s", addr)
	}

	log.Printf("✅ [Client] 节点上线: %s (当前可用: %d, MaxConns: %d)", addr, len(c.addrs), c.poolCfg.MaxConnsPerHost)
}

// removeNode 节点下线回调：关闭连接并移出池子
func (c *Client) removeNode(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr := value

	// 如果使用一致性哈希，需要从哈希环中移除
	if ch, ok := c.lb.(*ConsistentHash); ok {
		ch.mu.Lock()
		ch.ring.Remove(addr)
		ch.mu.Unlock()
		log.Printf("🔄 [Client] 一致性哈希环更新: 移除节点 %s", addr)
	}

	// 关闭连接
	if conn, ok := c.conns[addr]; ok {
		conn.Close()
		delete(c.conns, addr)
	}
	delete(c.clients, addr)

	// 从切片中移除地址
	newAddrs := make([]string, 0, len(c.addrs))
	for _, a := range c.addrs {
		if a != addr {
			newAddrs = append(newAddrs, a)
		}
	}
	c.addrs = newAddrs

	log.Printf("❌ [Client] 节点下线: %s (当前可用: %d)", addr, len(c.addrs))
}

// selectNode 选择一个节点（内部使用负载均衡器）
func (c *Client) selectNode(key string) (pb.KVServiceClient, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.addrs) == 0 {
		return nil, errors.New("no available kv-service nodes")
	}

	client, _, err := c.lb.Select(c.clients, c.addrs, &c.seq, key)
	return client, err
}

// Set 封装 Set 请求
func (c *Client) Set(key, value string) error {
	client, err := c.selectNode(key)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = client.Set(ctx, &pb.SetRequest{Key: key, Value: value})
	return err
}

// Get 封装 Get 请求
func (c *Client) Get(key string) (string, error) {
	client, err := c.selectNode(key)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := client.Get(ctx, &pb.GetRequest{Key: key})
	if err != nil {
		return "", err
	}
	return resp.Value, nil
}

// Del 封装 Del 请求
func (c *Client) Del(key string) error {
	client, err := c.selectNode(key)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.Del(ctx, &pb.DelRequest{Key: key})
	return err
}
