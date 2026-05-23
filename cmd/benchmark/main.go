package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"Flux-KV/internal/config"
	"Flux-KV/pkg/logger"
	"Flux-KV/pkg/network/client"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	concurrency = flag.Int("c", 100, "并发数 (Goroutines)")
	totalReq    = flag.Int("n", 500_000, "总请求数")
	mode        = flag.String("mode", "ap", "一致性模式: ap | cp")
	opMix       = flag.String("op", "set", "操作混合: set | get | del | mixed")
	readRatio   = flag.Int("read-ratio", 80, "mixed 模式下读操作比例 (0-100)")
)

type stats struct {
	success int64
	fail    int64
}

func main() {
	flag.Parse()

	config.InitConfig()
	logger.InitLogger()
	defer logger.Log.Sync()

	log := logger.Log

	// 校验模式
	var cm string
	switch *mode {
	case "cp":
		cm = "cp"
	case "ap":
		cm = "ap"
	default:
		log.Fatal("无效的模式，使用 ap 或 cp")
	}

	log.Info("开始压测",
		zap.Int("并发", *concurrency),
		zap.Int("总请求", *totalReq),
		zap.String("模式", *mode),
		zap.String("操作", *opMix),
	)

	// 直接连接 Gateway gRPC
	gatewayAddr := viper.GetString("gateway.address")
	if gatewayAddr == "" {
		gatewayAddr = "localhost:50051"
	}
	log.Info("Connecting to Gateway...", zap.String("addr", gatewayAddr))

	conn, err := client.NewDirectConn(gatewayAddr)
	if err != nil {
		log.Fatal("连接 Gateway 失败", zap.Error(err))
	}
	defer conn.Close()
	kvClient := client.NewClient(conn)

	// 启动 QPS 监控
	var s stats
	stopMonitor := make(chan struct{})
	go monitorQPS(&s, stopMonitor)

	// 启动压测 workers
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(*concurrency)

	reqPerWorker := *totalReq / *concurrency
	for i := 0; i < *concurrency; i++ {
		go func(id int) {
			defer wg.Done()
			runWorker(id, reqPerWorker, cm, &s, kvClient)
		}(i)
	}

	// 信号监听
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("收到中断信号，等待当前请求完成...")
	}()

	wg.Wait()
	close(stopMonitor)
	duration := time.Since(start)

	printReport(duration, &s)
}

func runWorker(id int, count int, mode string, s *stats, c *client.Client) {
	prefix := fmt.Sprintf("bench_%d_", id)
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s%d", prefix, i)

		ctx := context.Background()
		switch *opMix {
		case "set":
			err := c.SetWithMode(ctx, key, fmt.Sprintf("value_%d", rng.Intn(10000)), mode)
			record(s, err)
		case "get":
			_, err := c.GetWithMode(ctx, key, mode)
			record(s, err)
		case "del":
			err := c.DelWithMode(ctx, key, mode)
			record(s, err)
		case "mixed":
			if rng.Intn(100) < *readRatio {
				_, err := c.GetWithMode(ctx, key, mode)
				record(s, err)
			} else {
				err := c.SetWithMode(ctx, key, fmt.Sprintf("value_%d", rng.Intn(10000)), mode)
				record(s, err)
			}
		default:
			err := c.SetWithMode(ctx, key, fmt.Sprintf("value_%d", rng.Intn(10000)), mode)
			record(s, err)
		}
	}
}

func record(s *stats, err error) {
	if err != nil {
		atomic.AddInt64(&s.fail, 1)
	} else {
		atomic.AddInt64(&s.success, 1)
	}
}

func monitorQPS(s *stats, stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var last int64
	for {
		select {
		case <-ticker.C:
			cur := atomic.LoadInt64(&s.success)
			diff := cur - last
			last = cur
			fail := atomic.LoadInt64(&s.fail)
			fmt.Printf("QPS: %6d | 成功: %8d | 失败: %6d\n", diff, cur, fail)
		case <-stopCh:
			return
		}
	}
}

func printReport(d time.Duration, s *stats) {
	total := atomic.LoadInt64(&s.success) + atomic.LoadInt64(&s.fail)
	qps := float64(total) / d.Seconds()
	successRate := float64(0)
	if total > 0 {
		successRate = float64(atomic.LoadInt64(&s.success)) / float64(total) * 100
	}

	fmt.Println("\n╔════════════════════════════════════════════╗")
	fmt.Println("║           压测报告                         ║")
	fmt.Println("╠════════════════════════════════════════════╣")
	fmt.Printf("║ 耗时:         %26v ║\n", d)
	fmt.Printf("║ 总请求:       %26d ║\n", total)
	fmt.Printf("║ 成功:         %26d ║\n", atomic.LoadInt64(&s.success))
	fmt.Printf("║ 失败:         %26d ║\n", atomic.LoadInt64(&s.fail))
	fmt.Printf("║ 成功率:       %25.2f%% ║\n", successRate)
	fmt.Printf("║ 平均 QPS:     %25.2f ║\n", qps)
	fmt.Println("╚════════════════════════════════════════════╝")
}
