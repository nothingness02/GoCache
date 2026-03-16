package logger

import (
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Log 是一个全局变量，以后在任何地方引入这个包就可以直接用
var Log *zap.Logger

// 1. 定义访问日志的数据结构
type AccessLogEntry struct {
	Method    string
	Path      string
	Query     string
	Status    int
	ClientIP  string
	Latency   time.Duration
	UserAgent string
	Error     string
}

// 2. 定义缓冲区
// 如果日志产生速度 > 写入速度，缓冲满后会丢弃日志，保护服务
var accessLogChan = make(chan *AccessLogEntry, 2048)

func InitLogger() {
	// 获取配置中的日志配置
	logMode := viper.GetString("server.mode")

	var config zap.Config
	if logMode == "debug" {
		// 开发模式：日志是彩色的，方便看
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		// 生产模式：日志是 JSON 格式，速度快，方便机器收集
		config = zap.NewProductionConfig()
	}

	// 设置输出位置（默认标准输出）
	config.OutputPaths = []string{"stdout"}

	// 构建日志器
	var err error
	Log, err = config.Build()
	if err != nil {
		// 如果日志都起不来，那程序也没法跑了
		panic("❌ 日志初始化失败: " + err.Error())
	}

	// 3. 启动后台日志消费者 Goroutine
	go processLogWorker()

	Log.Info("✅ 日志系统初始化完成", zap.String("env", logMode))
}

// processLogWorker 后台消费协程：不停从 channel 取数据写日志
func processLogWorker() {
	for entry := range accessLogChan {
		// 根据状态码决定日志级别
		field := []zap.Field{
			zap.String("method", entry.Method),
			zap.String("path", entry.Path),
			zap.Int("status", entry.Status),
			zap.Duration("latency", entry.Latency),
			zap.String("ip", entry.ClientIP),
		}

		if entry.Error != "" {
			field = append(field, zap.String("error", entry.Error))
		}

		// 真正的写盘操作
		if entry.Status >= 500 {
			Log.Error("HTTP Server Error", field...)
		} else if entry.Status >= 400 {
			Log.Warn("HTTP Client Error", field...)
		} else {
			Log.Info("HTTP Access", field...)
		}
	}
}

// WriteAccessLog 公开的写入方法
func WriteAccessLog(entry *AccessLogEntry) {
	select {
	case accessLogChan <- entry:
		// 成功写入 channel
	default:
		// 略
	}
}