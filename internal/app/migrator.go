package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"Flux-KV/internal/storage"
	"Flux-KV/pkg/consistent"
	"Flux-KV/pkg/network/client"
	"Flux-KV/pkg/network/discovery"
)

// migrationItem 表示一条待迁移的数据
type migrationItem struct {
	key   string
	value string
	to    string // 目标节点地址
}

// Migrator 负责 AP 节点扩容时的后台数据迁移
// 定期扫描本地引擎，将因一致性哈希环变化而不再属于本节点的 key Push 到新节点
type Migrator struct {
	engine          storage.Engine
	disco           *discovery.Discovery
	meta            NodeMeta
	selfAddr        string
	deleteAfterPush bool        // 推送成功后是否本地删除（cut-over）
	batchSize       int         // 每批次最大推送数量
	delayPerItem    time.Duration // 每个 key 推送后的延迟，避免压垮目标节点

	stopCh chan struct{}
}

// NewMigrator 创建 Migrator
// engine: 本地存储引擎
// disco: Etcd 发现客户端
// meta: 当前节点元信息
// selfAddr: 当前节点对外 gRPC 地址（如 "ap-node-1:50052"）
func NewMigrator(engine storage.Engine, disco *discovery.Discovery, meta NodeMeta, selfAddr string) *Migrator {
	return &Migrator{
		engine:          engine,
		disco:           disco,
		meta:            meta,
		selfAddr:        selfAddr,
		deleteAfterPush: true, // 默认开启 cut-over，推送成功后本地删除
		batchSize:       100,
		delayPerItem:    time.Millisecond,
		stopCh:          make(chan struct{}),
	}
}

// Start 启动后台迁移 goroutine
func (m *Migrator) Start() {
	if m.meta.Mode != "ap" {
		return // 仅 AP 节点需要迁移
	}
	go m.run()
}

// Stop 停止迁移 goroutine
func (m *Migrator) Stop() {
	close(m.stopCh)
}

func (m *Migrator) run() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			if err := m.doMigration(); err != nil {
				log.Printf("[Migrator] migration cycle failed: %v", err)
			}
		}
	}
}

func (m *Migrator) doMigration() error {
	if m.disco == nil {
		return nil
	}

	// 1. 获取所有 AP 节点
	nodes, err := m.disco.ListNodes("/services/kv-service/")
	if err != nil {
		return fmt.Errorf("list nodes failed: %w", err)
	}

	var apAddrs []string
	for _, node := range nodes {
		if node.Mode == "ap" {
			apAddrs = append(apAddrs, node.Addr)
		}
	}
	if len(apAddrs) == 0 {
		return nil
	}

	// 2. 构建 active ring
	ring := consistent.New(150, nil)
	for _, addr := range apAddrs {
		ring.Add(addr)
	}

	// 3. 扫描本地引擎，找出不再属于本节点的 key
	var items []migrationItem

	m.engine.Scan(func(key string, val []byte) {
		target := ring.Get(key)
		if target != "" && target != m.selfAddr {
			items = append(items, migrationItem{key: key, value: string(val), to: target})
		}
	})

	if len(items) == 0 {
		return nil
	}

	log.Printf("[Migrator] found %d keys to migrate", len(items))

	// 4. 批量 Push 到目标节点
	// 按目标节点分组，减少连接数
	byTarget := make(map[string][]migrationItem)
	for _, it := range items {
		byTarget[it.to] = append(byTarget[it.to], it)
	}

	for target, batch := range byTarget {
		if err := m.pushBatch(target, batch); err != nil {
			log.Printf("[Migrator] push to %s failed: %v", target, err)
		}
	}

	return nil
}

func (m *Migrator) pushBatch(target string, batch []migrationItem) error {
	conn, err := client.NewDirectConn(target)
	if err != nil {
		return fmt.Errorf("connect to %s failed: %w", target, err)
	}
	defer conn.Close()

	cli := client.NewClient(conn)

	for i := 0; i < len(batch); i += m.batchSize {
		end := i + m.batchSize
		if end > len(batch) {
			end = len(batch)
		}
		chunk := batch[i:end]

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		for _, it := range chunk {
			if err := cli.InternalSet(ctx, it.key, it.value); err != nil {
				log.Printf("[Migrator] InternalSet %s -> %s failed: %v", it.key, target, err)
				continue
			}
			if m.deleteAfterPush {
				if err := m.engine.Delete(it.key); err != nil {
					log.Printf("[Migrator] local delete %s after push failed: %v", it.key, err)
				}
			}
			if m.delayPerItem > 0 {
				time.Sleep(m.delayPerItem)
			}
		}
		cancel()
	}
	return nil
}
