package main

import (
	"Flux-KV/pkg/client"
	"Flux-KV/pkg/consistent"
	"Flux-KV/pkg/discovery"
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

var etcdAddr = flag.String("etcd", "localhost:2379", "Etcd address")

func main() {
	flag.Parse()

	// 1. 初始化核心组件

	// 哈希环：负责计算 Key 归哪个节点管
	ring := consistent.New(20, nil)

	// 连接池：缓存每个节点的 TCP 连接
	clients := make(map[string]*client.Client)
	var mu sync.RWMutex

	// 2. 连接 Etcd 并启动监听
	fmt.Printf("🔍 正在连接 Etcd 注册中心 [%s]...\n", *etcdAddr)
	d, err := discovery.NewDiscovery([]string{*etcdAddr})
	if err != nil {
		log.Fatalf("❌ 无法连接 Etcd: %v", err)
	}
	defer d.Close()

	// 定义回调：当节点上线时
	addNode := func(key, addr string) {
		mu.Lock()
		defer mu.Unlock()

		// 1. 只有当连接不存在时才创建
		if _, exists := clients[addr]; !exists {
			cli, err := client.NewDirectClient(addr)
			if err != nil {
				log.Printf("❌ 无法连接新节点 [%s]: %v", addr, err)
				return
			}
			clients[addr] = cli
			// 加入哈希环
			ring.Add(addr)
			log.Printf("✅ [上线] 新节点加入: %s (当前总数: %d)", addr, len(clients))
		}
	}

	// 定义回调：当节点下线时
	removeNode := func(key, addr string) {
		mu.Lock()
		defer mu.Unlock()

		// 关闭连接并清理（addr 就是 value，直接使用）
		if cli, exists := clients[addr]; exists {
			cli.Close()
			delete(clients, addr)
			ring.Remove(addr)
			log.Printf("🚫 [下线] 节点移除: %s (当前总数: %d)", addr, len(clients))
		}
	}

	// 开始监听 /services/kv-service/ 前缀（与服务器注册的前缀保持一致）
	err = d.WatchService("/services/kv-service/", addNode, removeNode)
	if err != nil {
		log.Fatalf("❌ 监听服务失败: %v", err)
	}

	time.Sleep(1 * time.Second)

	// ===========================
	// 3. 启动交互式循环 (REPL)
	// ===========================
	fmt.Println("------------------------------------------------")
	fmt.Println("🚀 集群客户端已就绪! (输入 SET/GET/DEL 操作)")
	fmt.Println("------------------------------------------------")

	reader := bufio.NewReader(os.Stdin)

	for {
		// 打印提示符
		fmt.Print("Go-KV> ")

		// 读取用户输入的一行
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)

		// 处理空输入
		if text == "" {
			continue
		}

		// 解析命令
		parts := strings.Fields(text)
		cmd := strings.ToUpper(parts[0])

		if cmd == "EXIT" || cmd == "QUIT" {
			fmt.Println("👋 Bye!")
			break
		}

		// 路由逻辑：获取 Key
		if len(parts) < 2 {
			fmt.Println("⚠️  缺少 Key")
			continue
		}
		key := parts[1]

		// A. 计算路由 (Key -> Node Address)
		mu.RLock()
		if len(clients) == 0 {
			fmt.Println("⚠️  当前集群为空，无法处理请求！")
			mu.RUnlock()
			continue
		}
		nodeAddr := ring.Get(key)
		targetClient := clients[nodeAddr] // 获取对应的客户端连接
		mu.RUnlock()

		fmt.Printf("Testing: Key [%s] -> 路由到节点 [%s]\n", key, nodeAddr)

		// B. 执行命令
		switch cmd {
		case "SET":
			if len(parts) < 3 {
				fmt.Println("⚠️  用法: SET <key> <value>")
				continue
			}

			val := strings.Join(parts[2:], " ")

			err := targetClient.Set(key, val)
			if err != nil {
				fmt.Printf("❌ SET 错误: %v\n", err)
			} else {
				fmt.Println("OK")
			}

		case "GET":
			val, err := targetClient.Get(key)
			if err != nil {
				fmt.Printf("❌ GET 错误: %v\n", err)
			} else {
				// 模仿 Redis，输出加上引号
				fmt.Printf("\"%s\"\n", val)
			}

		case "DEL":
			key := parts[1]
			err := targetClient.Del(key)
			if err != nil {
				fmt.Printf("❌ DEL 错误: %v\n", err)
			} else {
				fmt.Println("(integer) 1") // 模仿 Redis 风格
			}

		case "EXIT", "QUIT":
			fmt.Println("👋 Bye!")
			return

		default:
			fmt.Printf("❌ 未知命令: %s\n", cmd)
		}
	}
}
