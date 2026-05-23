package main

import (
	"Flux-KV/internal/config"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/spf13/viper"
)

type EventType int

const (
	EventSet EventType = iota
	EventDel
)

type Event struct {
	Type  EventType   `json:"type"`
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

func main() {
	// 1. 初始化配置系统
	config.InitConfig()
	config.PrintConfig()

	// 2. 从配置读取 RabbitMQ 相关参数
	amqpURL := viper.GetString("rabbitmq.url")
	exchangeName := viper.GetString("cdc.exchange")
	queueName := viper.GetString("cdc.queue")
	logFileName := viper.GetString("cdc.log_path")
	consumerTag := "flux-cdc-consumer-1"

	// 3. 连接 RabbitMQ (建立连接 + 打开通道)
	conn, err := amqp.Dial(amqpURL)
	failOnError(err, "Failed to connect to RabbitMQ")

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")

	// 4. 声明交换机
	err = ch.ExchangeDeclare(
		exchangeName,
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)
	failOnError(err, "Failed to declare exchange")

	// 5. 声明队列
	q, err := ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	failOnError(err, "Failed to declare queue")

	// 6. 绑定队列到交换机
	err = ch.QueueBind(
		q.Name, "", exchangeName, false, nil,
	)
	failOnError(err, "Failed to bind queue")

	// 7. 注册消费者
	msgs, err := ch.Consume(
		q.Name,
		consumerTag,
		false, // 手动确认消息
		false,
		false,
		false,
		nil,
	)
	failOnError(err, "Failed to register consumer")

	// 8. 打开日志文件
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	failOnError(err, "Failed to open log file")

	log.Printf("[*] Waiting for CDC events. To exit press CTRL+C")

	// 9. 处理消息循环
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for d := range msgs {
			var event Event
			// 反序列化 JSON 消息体
			if err := json.Unmarshal(d.Body, &event); err != nil {
				log.Printf("Error decoding JSON: %s", err)
				d.Ack(false)
				continue
			}

			// 从 AMQP 消息头获取时间戳
			eventTime := d.Timestamp
			if eventTime.IsZero() {
				eventTime = time.Now()
			}

			if err := processEvent(logFile, event, eventTime); err != nil {
				log.Printf("❌ Write failed: %v", err)
			} else {
				d.Ack(false)
			}
		}
		log.Println("✅ 消息通道已关闭，消费者协程退出")
	}()

	// 10. 优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan // 阻塞等待信号

	log.Println("\n⚠️  收到退出信号，正在停止 Consumer...")

	// Step A: 停止接收新消息
	// 告诉 RabbitMQ：这个消费者下班了，别再发新消息过来
	// 这会导致 msgs 通道被关闭，从而让上面的 for 循环结束
	if err := ch.Cancel(consumerTag, false); err != nil {
		log.Printf("Error cancelling consumer: %s", err)
	}

	// Step B: 等待当前消息处理完
	log.Println("⏳ 等待现有消息处理完毕...")
	wg.Wait()

	// Step C: 资源清理
	log.Println("💾 正在刷盘日志文件...")
	logFile.Sync() // 强制落盘
	logFile.Close()

	ch.Close()
	conn.Close()
	log.Println("👋 CDC Consumer 安全退出")
}

func processEvent(f *os.File, e Event, t time.Time) error {
	timeStr := t.Format(time.RFC3339)
	var logLine string

	// 还原操作类型字符串
	op := "UNKNOWN"
	if e.Type == EventSet {
		op = "SET"
	} else if e.Type == EventDel {
		op = "DEL"
	}

	// 构造不同操作类型的日志行
	if e.Type == EventSet {
		valStr := fmt.Sprintf("%v", e.Value)
		logLine = fmt.Sprintf("[%s] [CDC_SYNC] %s key='%s' value_len=%d >> Persisted\n", timeStr, op, e.Key, len(valStr))
	} else {
		logLine = fmt.Sprintf("[%s] [CDC_SYNC] %s key='%s' >> Deleted\n", timeStr, op, e.Key)
	}

	// 写入日志文件
	if _, err := f.WriteString(logLine); err != nil {
		return err
	}
	fmt.Print(logLine) // 同时打印到控制台
	return nil
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", msg, err)
	}
}
