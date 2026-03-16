package main

import (
	"Flux-KV/pkg/discovery"
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "Flux-KV/api/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	concurrency = flag.Int("c", 100, "并发数 (Goroutines)")
	totalReq    = flag.Int("n", 500_000, "总请求数")
	endpoints   = flag.String("etcd", "localhost:2379", "Etcd 地址")

	// 统计指标
	successCount int64
	failCount    int64
)

// ===== 简易 Client 实现 =====

type SimpleClient struct {
	mu      sync.RWMutex
	clients map[string]pb.KVServiceClient // addr -> 客户端存根
	conns   map[string]*grpc.ClientConn   // addr -> 连接
	addrs   []string
	seq     uint64
}

// 将 Docker 内部地址转换为本地端口
func mapToLocalAddr(internalAddr string) string {
	// kv-server-1:50052 -> localhost:50052
	// kv-server-2:50052 -> localhost:50053
	// kv-server-3:50052 -> localhost:50054
	parts := strings.Split(internalAddr, ":")
	if len(parts) != 2 {
		return internalAddr
	}
	hostname := parts[0]

	// 根据 hostname 映射端口
	switch hostname {
	case "kv-server-1":
		return "localhost:50052"
	case "kv-server-2":
		return "localhost:50053"
	case "kv-server-3":
		return "localhost:50054"
	default:
		// 其他情况保持原样
		return internalAddr
	}
}

func NewSimpleClient() *SimpleClient {
	return &SimpleClient{
		clients: make(map[string]pb.KVServiceClient),
		conns:   make(map[string]*grpc.ClientConn),
		addrs:   make([]string, 0),
	}
}

// addNode 添加节点（地址转换）
func (c *SimpleClient) addNode(key, internalAddr string) {
	// 地址转换：Docker 内部地址 -> 本地地址
	addr := mapToLocalAddr(internalAddr)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.clients[addr]; ok {
		return
	}

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Printf("❌ 连接节点失败 %s: %v", addr, err)
		return
	}

	rpcClient := pb.NewKVServiceClient(conn)
	c.clients[addr] = rpcClient
	c.conns[addr] = conn
	c.addrs = append(c.addrs, addr)

	log.Printf("✅ 节点上线: %s (原: %s)", addr, internalAddr)
}

// removeNode 移除节点
func (c *SimpleClient) removeNode(key, internalAddr string) {
	addr := mapToLocalAddr(internalAddr)

	c.mu.Lock()
	defer c.mu.Unlock()

	// 关闭连接
	if conn, ok := c.conns[addr]; ok {
		conn.Close()
		delete(c.conns, addr)
	}
	delete(c.clients, addr)

	newAddrs := make([]string, 0, len(c.addrs))
	for _, a := range c.addrs {
		if a != addr {
			newAddrs = append(newAddrs, a)
		}
	}
	c.addrs = newAddrs

	log.Printf("❌ 节点下线: %s", addr)
}

// Set 执行 Set 请求
func (c *SimpleClient) Set(key, value string) error {
	c.mu.RLock()
	if len(c.addrs) == 0 {
		c.mu.RUnlock()
		return fmt.Errorf("no available nodes")
	}

	// 简单轮询
	next := atomic.AddUint64(&c.seq, 1)
	addr := c.addrs[next%uint64(len(c.addrs))]
	rpcClient := c.clients[addr]
	c.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rpcClient.Set(ctx, &pb.SetRequest{Key: key, Value: value})
	return err
}

func main() {
	flag.Parse()
	fmt.Printf("🚀 开始压测: %d 并发, 目标 %d 请求, Etcd: %s\n", *concurrency, *totalReq, *endpoints)

	d, err := discovery.NewDiscovery([]string{*endpoints})
	if err != nil {
		log.Fatalf("Failed to create discovery: %v", err)
		return
	}

	// 创建简易 Client
	client := NewSimpleClient()

	// 启动服务发现监听
	prefix := "/services/kv-service/"
	err = d.WatchService(prefix, client.addNode, client.removeNode)
	if err != nil {
		log.Fatalf("Failed to watch service: %v", err)
		return
	}

	// 2. 启动监控协程（每秒打印 QPS）
	go monitor()

	// 3. 启动并发 Workers
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(*concurrency)

	// 计算每个 Worker 需要完成的任务量
	reqPerWorker := *totalReq / *concurrency
	for i := 0; i < *concurrency; i++ {
		go func(workerID int) {
			defer wg.Done()
			runWorker(reqPerWorker, workerID, client)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// 4. 最终报告
	printReport(duration)
}

// 模拟单个用户的行为
func runWorker(count int, workerID int, client *SimpleClient) {
	// 预先生成随机 Key 前缀，模拟不同数据
	keyPrefix := fmt.Sprintf("user_%d_,", workerID)
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s%d", keyPrefix, i)
		value := fmt.Sprintf("value_%d", rand.Intn(1000))

		err := client.Set(key, value) // 这里只测试 Set 接口，你也可以扩展测试 Get 和 Del
		// 测试 Set
		if err != nil {
			atomic.AddInt64(&failCount, 1)
			// 👇👇👇 必须把这行注释打开！让我们看到报错信息！👇👇👇
			log.Printf("Set Error: %v", err)
		} else {
			atomic.AddInt64(&successCount, 1)
		}
	}
}

// 监控器：每秒输出当前 QPS
func monitor() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastCount int64
	for range ticker.C {
		current := atomic.LoadInt64(&successCount)
		diff := current - lastCount
		lastCount = current
		fmt.Printf("🔥 QPS: %d | 成功: %d | 失败: %d\n", diff, current, atomic.LoadInt64(&failCount))
	}
}

func printReport(d time.Duration) {
	total := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&failCount)

	qps := float64(total) / d.Seconds()

	fmt.Println("\n--- 🏁 压测报告 ---")
	fmt.Printf("耗时: %v\n", d)
	fmt.Printf("总请求: %d\n", total)
	fmt.Printf("成功率: %.2f%%\n", float64(successCount)/float64(total)*100)
	fmt.Printf("平均 QPS: %.2f\n", qps)
	fmt.Println("-------------------")
}
