package aof

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

const bufferSize = 1000 // AOF channel 缓冲区大小

// Cmd 定义了写入文件的每一行数据的格式
type Cmd struct {
	Type  string `json:"type"`  // 操作类型
	Key   string `json:"key"`   // 键
	Value string `json:"value"` // 值
}

type AofHandler struct {
	file          *os.File
	rd            *bufio.Reader
	ch            chan Cmd
	mu            sync.Mutex     // 互斥锁，保证多协程写入文件时不会串行混杂
	wg            sync.WaitGroup // 等待消费者结束
	stop          chan struct{}  // 停止信号

	batch         []Cmd
	batchSize     int
	flushInterval time.Duration
}

// NewAofHandler 初始化 AOF 模块
func NewAofHandler(filename string, batchSize int, flushInterval time.Duration) (*AofHandler, error) {
	if batchSize <= 0 {
		batchSize = 100
	}
	if flushInterval <= 0 {
		flushInterval = 10 * time.Millisecond
	}

	// os.O_APPEND: 追加模式
	// os.O_CREATE: 文件不存在则创建
	// os.O_RDWR: 读写模式
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	handler := &AofHandler{
		file:          f,
		rd:            bufio.NewReader(f),
		ch:            make(chan Cmd, bufferSize),
		stop:          make(chan struct{}),
		batch:         make([]Cmd, 0, batchSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
	}
	handler.startConsumer()
	return handler, nil
}

func (handler *AofHandler) startConsumer() {
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		ticker := time.NewTicker(handler.flushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-handler.stop:
				ticker.Stop()
				handler.flushBatch()
				// 处理剩余数据
				for {
					select {
					case cmd := <-handler.ch:
						handler.batch = append(handler.batch, cmd)
						if len(handler.batch) >= handler.batchSize {
							handler.flushBatch()
						}
					default:
						handler.flushBatch()
						return
					}
				}
			case cmd := <-handler.ch:
				handler.batch = append(handler.batch, cmd)
				if len(handler.batch) >= handler.batchSize {
					handler.flushBatch()
				}
			case <-ticker.C:
				handler.flushBatch()
			}
		}
	}()
}

// AsyncWrite 将命令发送到 channel，由后台协程异步写入文件
func (handler *AofHandler) AsyncWrite(c Cmd) error {
	select {
	case <-handler.stop:
		return errors.New("AOF handler closed")
	case handler.ch <- c:
		return nil
	}
}

// SyncWrite 将单条命令同步序列化并追加到文件末尾
func (handler *AofHandler) SyncWrite(c Cmd) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = handler.file.Write(data)
	if err != nil {
		return err
	}
	_, err = handler.file.WriteString("\n")
	return err
}

// flushBatch 将当前 batch 一次性写入文件，减少 syscall 次数
func (handler *AofHandler) flushBatch() {
	if len(handler.batch) == 0 {
		return
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	var buf bytes.Buffer
	for _, c := range handler.batch {
		data, err := json.Marshal(c)
		if err != nil {
			log.Printf("[AOF] Marshal error: %v", err)
			continue
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	if buf.Len() > 0 {
		_, err := handler.file.Write(buf.Bytes())
		if err != nil {
			log.Printf("[AOF] Write error: %v", err)
		}
	}

	handler.batch = handler.batch[:0]
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
			log.Printf("[AOF] Failed to parse line, skipping: %v | line: %s", err, string(line))
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
	// 1. 停止后台消费者协程，等待其处理完剩余数据
	close(handler.stop)
	handler.wg.Wait()

	// 2. 强制刷盘并关闭文件
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if err := handler.file.Sync(); err != nil {
		return err
	}

	return handler.file.Close()
}
