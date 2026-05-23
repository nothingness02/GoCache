package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/viper"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Target struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

var (
	etcdEndpoints   []string
	outputPath      string
	watchPrefix     string
	refreshInterval time.Duration
)

func init() {
	// 使用 viper 读取环境变量
	viper.SetEnvPrefix("FLUX")
	viper.AutomaticEnv()

	// 默认值
	viper.SetDefault("etcd.endpoints", []string{"etcd:2379"})
}

func main() {
	// 解析命令行参数
	etcdFlag := flag.String("etcd", "", "Etcd endpoints (comma separated)")
	flag.StringVar(&outputPath, "output", "/etc/prometheus/targets/kv-server.json", "Output file path")
	flag.StringVar(&watchPrefix, "prefix", "/services/kv-service/", "Service prefix to watch")
	flag.DurationVar(&refreshInterval, "interval", 15*time.Second, "Refresh interval")
	flag.Parse()

	// 优先使用命令行参数，其次使用环境变量
	if *etcdFlag != "" {
		etcdEndpoints = strings.Split(*etcdFlag, ",")
	} else {
		etcdEndpoints = viper.GetStringSlice("etcd.endpoints")
	}

	log.Println("🚀 Prometheus SD Service starting...")
	log.Printf("   Etcd endpoints: %v", etcdEndpoints)
	log.Printf("   Watch prefix: %s", watchPrefix)
	log.Printf("   Output path: %s", outputPath)
	log.Printf("   Refresh interval: %v", refreshInterval)

	// 确保输出目录存在
	if err := os.MkdirAll("/etc/prometheus/targets", 0755); err != nil {
		log.Printf("⚠️  Warning: Failed to create directory: %v", err)
	}

	// 创建 HTTP 服务器用于健康检查和调试
	go func() {
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})
		http.HandleFunc("/targets", func(w http.ResponseWriter, r *http.Request) {
			targets := fetchTargetsFromEtcd()
			data, _ := json.MarshalIndent(targets, "", "  ")
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
		})
		log.Println("🌐 HTTP server listening on :8080")
		http.ListenAndServe(":8080", nil)
	}()

	// 初始加载
	updateTargetsFile()

	// 定时刷新
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	// 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			updateTargetsFile()
		case <-quit:
			log.Println("👋 Shutting down...")
			return
		}
	}
}

func fetchTargetsFromEtcd() []Target {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Printf("❌ Failed to connect to Etcd: %v", err)
		return nil
	}
	defer cli.Close()

	// 获取所有服务
	resp, err := cli.Get(ctx, watchPrefix, clientv3.WithPrefix())
	if err != nil {
		log.Printf("❌ Failed to get services: %v", err)
		return nil
	}

	var targets []Target
	for _, kv := range resp.Kvs {
		addr := string(kv.Value)
		key := string(kv.Key)

		// 提取服务名称
		serviceName := key
		if len(key) > len(watchPrefix) {
			serviceName = key[len(watchPrefix):]
		}
		addr = strings.Split(addr, ":")[0]
		// 每个 kv-server 需要添加 metrics 端口 9090
		metricsAddr := addr + ":9090"

		targets = append(targets, Target{
			Targets: []string{metricsAddr},
			Labels: map[string]string{
				"service":     "kv-service",
				"instance":    serviceName,
				"__address__": metricsAddr,
			},
		})
	}

	return targets
}

func updateTargetsFile() {
	targets := fetchTargetsFromEtcd()
	if targets == nil {
		log.Println("⚠️  No targets fetched, skipping update")
		return
	}

	data, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		log.Printf("❌ Failed to marshal targets: %v", err)
		return
	}

	// 写入文件（原子操作：先写临时文件，再重命名）
	tmpPath := outputPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		log.Printf("❌ Failed to write targets file: %v", err)
		return
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		log.Printf("❌ Failed to rename targets file: %v", err)
		return
	}

	fmt.Printf("✅ Updated %d targets to %s\n", len(targets), outputPath)
}
