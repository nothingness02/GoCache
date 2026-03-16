package aof

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sync"
)

const bufferSize = 1000 // AOF 写入缓冲区大小

// Cmd 定义了写入文件的每一行数据的格式
type Cmd struct {
	Type  string `json:"type"`  // 操作类型
	Key   string `json:"key"`   // 键
	Value any    `json:"value"` // 值
}

type AofHandler struct {
	file *os.File
	rd   *bufio.Reader
	ch   chan Cmd
	mu   sync.Mutex     // 互斥锁，保证多协程写入文件时不会串行混杂  用来同步写入文件的操作
	wg   sync.WaitGroup // 等待消费者结束
	stop chan struct{}  // 停止信号
}

// NewAofHandler 初始化 AOF 模块
func NewAofHandler(filename string) (*AofHandler, error) {
	// os.O_APPEND: 追加模式
	// os.O_CREATE: 文件不存在则创建
	// os.O_RDWR: 读写模式
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	handler := &AofHandler{
		file: f,
		rd:   bufio.NewReader(f),
		ch:   make(chan Cmd, bufferSize), // 有缓冲 channel
		stop: make(chan struct{}),
	}
	handler.startConsumer()
	return handler, nil
}

func (handler *AofHandler) startConsumer() {
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		for {
			select {
			case <-handler.stop:
				// 处理剩余数据
				for {
					select {
					case cmd := <-handler.ch:
						handler.SyncWrite(cmd)
					default:
						return
					}
				}
			case cmd := <-handler.ch:
				handler.SyncWrite(cmd)
			}
		}
	}()
}

// AsyncWrite 将命令发送到 channel，由后台协程异步写入文件
func (handler *AofHandler) AsyncWrite(c Cmd) error {
	select {
	case handler.ch <- c:
	default:
		// channel 满了，直接丢弃命令并记录日志（可以改成其他策略）
		log.Printf(" ⚠️ Buffer full, dropping event: %s", c.Key)
		return errors.New("AOF buffer full")
	}
	return nil
}

// Write 将命令序列化并追加到文件末尾
// 这里是 IO 瓶颈所在，后续可以用 channel 做异步刷盘优化
func (handler *AofHandler) SyncWrite(c Cmd) error {
	handler.mu.Lock() // 加锁，保证多协程写入时数据不混杂
	defer handler.mu.Unlock()

	// 1. 序列化为 JSON 字节数组
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	// 2. 写入 JSON 数据
	_, err = handler.file.Write(data)
	if err != nil {
		return err
	}

	// 3. 写入换行符
	_, err = handler.file.WriteString("\n")
	return err
}

// ReadAll 读取文件中的所有历史命令，用于启动时恢复
func (handler *AofHandler) ReadAll() ([]Cmd, error) {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	var cmds []Cmd

	// 1. 将文件指针移到开头
	_, err := handler.file.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	// 2. 逐行扫描文件
	scanner := bufio.NewScanner(handler.file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var cmd Cmd
		// 反序列化当前行到 Cmd 结构体
		err := json.Unmarshal(line, &cmd)
		if err != nil {
			continue
		}
		cmds = append(cmds, cmd)
	}

	// 检查扫描过程中的错误
	if err := scanner.Err(); err != nil && err != io.EOF {
		return nil, err
	}

	return cmds, nil
}

// Close 关闭文件资源
func (handler *AofHandler) Close() error {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	// 强制刷盘
	if err := handler.file.Sync(); err != nil {
		return err
	}

	return handler.file.Close()
}
