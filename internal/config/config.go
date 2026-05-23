package config

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// ===== 配置结构体定义 =====

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Raft     RaftConfig     `mapstructure:"raft"`
	AOF      AOFConfig      `mapstructure:"aof"`
	Etcd     EtcdConfig     `mapstructure:"etcd"`
	RabbitMQ RabbitMQConfig `mapstructure:"rabbitmq"`
	Jaeger   JaegerConfig   `mapstructure:"jaeger"`
	Pprof    PprofConfig    `mapstructure:"pprof"`
	CDC      CDCConfig      `mapstructure:"cdc"`
	Log      LogConfig      `mapstructure:"log"`
	Gateway  GatewayConfig  `mapstructure:"gateway"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

type StorageConfig struct {
	ShardType  string `mapstructure:"shard_type"`  // "map" | "zerogc"
	LockerType string `mapstructure:"locker_type"` // "global" | "sharded"
	ShardCount int    `mapstructure:"shard_count"` // 分片数量，global 锁时忽略
	ShardSize  int    `mapstructure:"shard_size"`  // 单个 ZeroGCShard 容量（字节）
}

type RaftConfig struct {
	Enabled  bool     `mapstructure:"enabled"`
	NodeID   string   `mapstructure:"node_id"`
	GroupID  string   `mapstructure:"group_id"`
	Peers    []string `mapstructure:"peers"`     // 同组其他节点地址列表
	BindAddr string   `mapstructure:"bind_addr"` // Raft RPC 监听地址
	DataDir  string   `mapstructure:"data_dir"`  // Raft 日志存储目录
}

type AOFConfig struct {
	Filename        string `mapstructure:"filename"`
	AppendFsync     string `mapstructure:"append_fsync"`
	BatchSize       int    `mapstructure:"batch_size"`
	FlushIntervalMs int    `mapstructure:"flush_interval_ms"`
}

type EtcdConfig struct {
	Endpoints []string `mapstructure:"endpoints"`
}

type RabbitMQConfig struct {
	URL string `mapstructure:"url"`
}

type JaegerConfig struct {
	Endpoint string `mapstructure:"endpoint"`
}

type PprofConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`
}

type CDCConfig struct {
	Exchange string `mapstructure:"exchange"`
	Queue    string `mapstructure:"queue"`
	LogPath  string `mapstructure:"log_path"`
}

type LogConfig struct {
	Level    string              `mapstructure:"level"`
	Encoding string              `mapstructure:"encoding"`
	Sampling *zap.SamplingConfig `mapstructure:"sampling"`
}

type GatewayConfig struct {
	GrpcPort       int                  `mapstructure:"grpc_port"`
	MetricsPort    int                  `mapstructure:"metrics_port"`
	RateLimiter    RateLimiterConfig    `mapstructure:"rate_limiter"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

type RateLimiterConfig struct {
	Enabled bool    `mapstructure:"enabled"`
	Rate    float64 `mapstructure:"rate"`
	Burst   int     `mapstructure:"burst"`
}

type CircuitBreakerConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	WindowSize       time.Duration `mapstructure:"window_size"`
	FailureThreshold float64       `mapstructure:"failure_threshold"`
	MinCalls         int           `mapstructure:"min_calls"`
	Cooldown         time.Duration `mapstructure:"cooldown"`
	HalfOpenMaxCalls int           `mapstructure:"half_open_max_calls"`
	SuccessThreshold int           `mapstructure:"success_threshold"`
}

// ===== 初始化函数 =====

// InitConfig 初始化配置，支持环境变量覆盖
func InitConfig() {
	// 1. 设置环境变量前缀和自动映射
	viper.SetEnvPrefix("FLUX")                             // 所有环境变量需要 FLUX_ 前缀
	viper.AutomaticEnv()                                   // 自动扫描环境变量
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_")) // 点(.)替换为下划线(_)

	// 2. 设置 YAML 配置文件位置和格式
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath("../../configs")

	// 3. 设置所有默认值（当环境变量和 YAML 都不存在时使用）
	setDefaults()

	// 4. 读取 YAML 配置文件（如果存在）
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("⚠️  警告：读取配置文件失败，将使用默认值和环境变量: %v\n", err)
	} else {
		log.Println("✅ 配置文件加载成功！")
	}

	log.Println("✅ 完成！优先级：环境变量 > config.yaml > 默认值")
}

// setDefaults 设置所有配置项的默认值
func setDefaults() {
	// Server
	viper.SetDefault("server.port", 50052)
	viper.SetDefault("server.mode", "debug")

	// Storage
	viper.SetDefault("storage.shard_type", "zerogc")
	viper.SetDefault("storage.locker_type", "sharded")
	viper.SetDefault("storage.shard_count", 256)
	viper.SetDefault("storage.shard_size", 10*1024*1024) // 10MB

	// Raft
	viper.SetDefault("raft.enabled", false)
	viper.SetDefault("raft.node_id", "node-1")
	viper.SetDefault("raft.group_id", "cp-group-1")
	viper.SetDefault("raft.peers", []string{})
	viper.SetDefault("raft.bind_addr", ":12000")
	viper.SetDefault("raft.data_dir", "/app/data/raft")

	// AOF
	viper.SetDefault("aof.filename", "/app/data/go-kv.aof")
	viper.SetDefault("aof.append_fsync", "everysec")
	viper.SetDefault("aof.batch_size", 100)
	viper.SetDefault("aof.flush_interval_ms", 10)

	// Etcd
	viper.SetDefault("etcd.endpoints", []string{"localhost:2379"})

	// RabbitMQ
	viper.SetDefault("rabbitmq.url", "amqp://guest:guest@localhost:5672/")

	// Jaeger
	viper.SetDefault("jaeger.endpoint", "localhost:4317")

	// Pprof
	viper.SetDefault("pprof.enabled", false)
	viper.SetDefault("pprof.port", 6060)

	// CDC
	viper.SetDefault("cdc.exchange", "flux_kv_events")
	viper.SetDefault("cdc.queue", "flux_cdc_file_logger")
	viper.SetDefault("cdc.log_path", "/app/logs/flux_cdc.log")

	// Log
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.encoding", "console")
	viper.SetDefault("log.sampling.initial", 100)
	viper.SetDefault("log.sampling.thereafter", 100)

	// Gateway
	viper.SetDefault("gateway.grpc_port", 50051)
	viper.SetDefault("gateway.metrics_port", 9090)
	viper.SetDefault("gateway.rate_limiter.enabled", true)
	viper.SetDefault("gateway.rate_limiter.rate", 1000.0)
	viper.SetDefault("gateway.rate_limiter.burst", 2000)
	viper.SetDefault("gateway.circuit_breaker.enabled", true)
	viper.SetDefault("gateway.circuit_breaker.window_size", 10*time.Second)
	viper.SetDefault("gateway.circuit_breaker.failure_threshold", 0.5)
	viper.SetDefault("gateway.circuit_breaker.min_calls", 5)
	viper.SetDefault("gateway.circuit_breaker.cooldown", 5*time.Second)
	viper.SetDefault("gateway.circuit_breaker.half_open_max_calls", 3)
	viper.SetDefault("gateway.circuit_breaker.success_threshold", 2)
}

// ===== 工具函数 =====

// GetServiceIP 返回服务注册到 Etcd 时使用的 IP 地址
// 优先级：FLUX_POD_IP 环境变量 > hostname 解析 > 网卡 IP > 默认 localhost
func GetServiceIP() string {
	// 优先级 1: 检查环境变量 FLUX_POD_IP（docker-compose 中明确指定）
	if ip := os.Getenv("FLUX_POD_IP"); ip != "" {
		log.Printf("✅ 使用 FLUX_POD_IP 环境变量: %s\n", ip)
		return ip
	}

	// 优先级 2: 通过 hostname 解析 IP（在 Docker Compose 中自动设置）
	hostname, err := os.Hostname()
	if err == nil {
		ips, err := net.LookupIP(hostname)
		if err == nil && len(ips) > 0 {
			for _, ip := range ips {
				if !ip.IsLoopback() {
					log.Printf("✅ 通过 hostname '%s' 解析到 IP: %s\n", hostname, ip)
					return ip.String()
				}
			}
		}
	}

	// 优先级 3: 扫描第一个非回环网卡的 IP
	if ip := getFirstNonLoopbackIP(); ip != "" {
		log.Printf("✅ 检测到非回环网卡 IP: %s\n", ip)
		return ip
	}

	// 最后兜底
	log.Println("⚠️  警告：无法获取有效 IP，使用默认值 localhost")
	return "localhost"
}

// getFirstNonLoopbackIP 获取第一个非回环网卡的 IP
func getFirstNonLoopbackIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		// 跳过禁用的网卡
		if (iface.Flags & net.FlagUp) == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}

			// 优先返回 IPv4
			if ip.To4() != nil {
				return ip.String()
			}
		}
	}

	return ""
}

// GetConfig 返回解析后的完整配置对象
func GetConfig() *Config {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		log.Fatalf("❌ 配置解析失败: %v\n", err)
	}
	return &cfg
}

// PrintConfig 打印当前生效的所有配置（调试用）
func PrintConfig() {
	cfg := GetConfig()
	fmt.Printf("\n╔════════════════════════════════════════╗\n")
	fmt.Printf("║       当前 Flux-KV 配置信息           ║\n")
	fmt.Printf("╚════════════════════════════════════════╝\n\n")
	fmt.Printf("📡 Server:\n")
	fmt.Printf("   Port: %d\n", cfg.Server.Port)
	fmt.Printf("   Mode: %s\n\n", cfg.Server.Mode)

	fmt.Printf("💾 AOF:\n")
	fmt.Printf("   Filename: %s\n", cfg.AOF.Filename)
	fmt.Printf("   AppendFsync: %s\n\n", cfg.AOF.AppendFsync)

	fmt.Printf("🔗 Etcd:\n")
	fmt.Printf("   Endpoints: %v\n\n", cfg.Etcd.Endpoints)

	fmt.Printf("🐰 RabbitMQ:\n")
	fmt.Printf("   URL: %s\n\n", maskSensitiveURL(cfg.RabbitMQ.URL))

	fmt.Printf("🕵️  Jaeger:\n")
	fmt.Printf("   Endpoint: %s\n\n", cfg.Jaeger.Endpoint)

	fmt.Printf("⚙️  Pprof:\n")
	fmt.Printf("   Enabled: %v\n", cfg.Pprof.Enabled)
	fmt.Printf("   Port: %d\n\n", cfg.Pprof.Port)

	fmt.Printf("📝 CDC:\n")
	fmt.Printf("   Exchange: %s\n", cfg.CDC.Exchange)
	fmt.Printf("   Queue: %s\n", cfg.CDC.Queue)
	fmt.Printf("   LogPath: %s\n\n", cfg.CDC.LogPath)

	fmt.Printf("📋 Log:\n")
	fmt.Printf("   Level: %s\n", cfg.Log.Level)
	fmt.Printf("   Encoding: %s\n\n", cfg.Log.Encoding)
}

// maskSensitiveURL 隐藏 URL 中的密码（调试用）
func maskSensitiveURL(url string) string {
	if idx := strings.Index(url, "://"); idx >= 0 {
		if atIdx := strings.Index(url[idx+3:], "@"); atIdx >= 0 {
			return url[:idx+3] + "***:***@" + url[idx+3+atIdx+1:]
		}
	}
	return url
}
