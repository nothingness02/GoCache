package event

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// EventType 定义事件类型
type EventType int

const (
	EventSet EventType = iota // 0: 写入/更新
	EventDel                  // 1: 删除
)

// Event 定义了传送带上的盘子里装什么
type Event struct {
	Type  EventType `json:"type"`
	Key   string    `json:"key"`
	Value any       `json:"value"`
}

// EventBus 事件总线核心结构
type EventBus struct {
	// 缓冲通道
	ch            chan Event
	rabbitConn    *amqp.Connection
	exchangeName  string
	mu            sync.Mutex     // 保护日志输出的同步
	wg            sync.WaitGroup // 等待消费者协程结束
	consumerCount int            // 消费者数量
	stopCh        chan struct{}  // 停止信号
}

// NewEventBus 初始化传送带
// bufferSize: 缓冲区大小
// consumerCount: 消费者数量，建议设置为 CPU 核数
func NewEventBus(bufferSize int, mqURL string, consumerCount int) (*EventBus, error) {
	// 1. 连接 RabbitMQ
	conn, err := amqp.Dial(mqURL)
	if err != nil {
		return nil, err
	}

	// 2. 打开一个 Channel (AMQP 的概念)
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	// 3. 声明交换机（Exchange）
	exchangeName := "flux_kv_events"
	err = ch.ExchangeDeclare(
		exchangeName, // name
		"fanout",     // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	return &EventBus{
		ch:            make(chan Event, bufferSize),
		rabbitConn:    conn,
		exchangeName:  exchangeName,
		consumerCount: consumerCount,
		stopCh:        make(chan struct{}),
	}, nil
}

// Publish 投递事件（阻塞等待，直到有消费者处理）
func (b *EventBus) Publish(e Event) {
	b.ch <- e
}

// StartConsumer 启动多个消费者协程
func (b *EventBus) StartConsumer() {
	for i := 0; i < b.consumerCount; i++ {
		b.wg.Add(1)
		go func(id int) {
			defer b.wg.Done()
			log.Printf("[EventBus] Consumer %d started.", id)
			rabbitCh, err := b.rabbitConn.Channel()
			if err != nil {
				log.Printf("❌ [EventBus] Consumer %d failed to open RabbitMQ channel: %v", id, err)
				return
			}
			defer rabbitCh.Close()
			for {
				select {
				case <-b.stopCh:
					// 处理剩余数据
					for {
						select {
						case e := <-b.ch:
							b.publishToRabbitMQ(e, rabbitCh)
						default:
							log.Printf("[EventBus] Consumer %d stopped.", id)
							return
						}
					}
				case e, ok := <-b.ch:
					if !ok {
						log.Printf("[EventBus] Consumer %d stopped.", id)
						return
					}
					b.publishToRabbitMQ(e, rabbitCh)
				}
			}
		}(i)
	}
}

// publishToRabbitMQ 发布消息到 RabbitMQ
func (b *EventBus) publishToRabbitMQ(e Event, rabbitCh *amqp.Channel) {
	// 1. 序列化消息（转成 JSON）
	body, err := json.Marshal(e)
	if err != nil {
		log.Printf("[EventBus] Json Marshal error: %v", err)
		return
	}

	// 2. 发送到 RabbitMQ
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = rabbitCh.PublishWithContext(ctx,
		b.exchangeName, // exchange
		"",             // routing key
		false,          // mandatory
		false,          // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
			Timestamp:   time.Now(),
		})
	cancel()

	if err != nil {
		log.Printf("❌ [RabbitMQ] Publish failed: %v", err)
	}
}

// Close 优雅关闭
func (b *EventBus) Close() error {
	// 1. 发送停止信号给所有消费者
	close(b.stopCh)

	// 2. 等待消费者协程把缓冲区数据全部发给 RabbitMQ
	log.Println("[EventBus] Waiting for consumers to finish...")
	b.wg.Wait()

	// 3. 关闭 channel
	close(b.ch)
	if b.rabbitConn != nil {
		return b.rabbitConn.Close()
	}
	return nil
}

func opStr(t EventType) string {
	if t == EventSet {
		return "SET"
	}
	return "DEL"
}
