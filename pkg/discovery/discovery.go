package discovery

import (
	"context"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Discovery 负责从 Etcd 发现服务
type Discovery struct {
	cli *clientv3.Client
}

// NewDiscovery 创建发现服务
func NewDiscovery(endpoints []string) (*Discovery, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return &Discovery{cli: cli}, nil
}

// WatchService 核心方法：初始化列表 + 监听变化
// prefix: 监听的前缀 (例如 /kv-service/)
// setFunc: 新增/修改节点时的回调函数
// delFunc: 删除节点时的回调函数
func (d *Discovery) WatchService(prefix string, setFunc, delFunc func(key, value string)) error {
	// 1. 先查当前已有的节点
	resp, err := d.cli.Get(context.Background(), prefix, clientv3.WithPrefix())
	if err != nil {
		return err
	}

	// 遍历现有的，先加载进去
	for _, kv := range resp.Kvs {
		if setFunc != nil {
			setFunc(string(kv.Key), string(kv.Value))
		}
	}

	// 2. 开启监听协程
	go func() {
		watchChan := d.cli.Watch(context.Background(), prefix, clientv3.WithPrefix())

		log.Println("👀 开始监听 Etcd 服务变化...")

		for watchResp := range watchChan {
			for _, ev := range watchResp.Events {
				key := string(ev.Kv.Key)
				val := string(ev.Kv.Value)

				switch ev.Type {
				case clientv3.EventTypePut:
					// Server 上线/更新
					log.Printf("🔥 [Discovery] 节点上线: %s", key)
					if setFunc != nil {
						setFunc(key, val)
					}
				case clientv3.EventTypeDelete:
					// Server 下线/过期
					log.Printf("❌ [Discovery] 节点下线: %s", key)
					if delFunc != nil {
						delFunc(key, val)
					}
				}
			}
		}
	}()
	return nil
}

func (d *Discovery) Close() {
	d.cli.Close()
}