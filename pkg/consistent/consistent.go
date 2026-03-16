package consistent

import (
	"hash/crc32"
	"slices"
	"sort"
	"strconv"
	"sync"
)

// Hash 定义哈希函数类型
type Hash func(data []byte) uint32

type Map struct {
	hash    Hash		// 哈希函数
	replicas int		// 虚拟节点倍数
	keys    []int		// 哈希环，存储虚拟节点的哈希值
	hashMap map[int]string // 虚拟节点与真实节点的映射表
	mu  sync.RWMutex	// 读写锁，保证并发安全
}

// New 创建一个一致性哈希算法的 Map 实例
func New(replicas int, fn Hash) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		// 默认使用 CRC32 算法
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

// Add 添加真实节点到哈希环
// keys: 真实节点的名称，如 "192.168.1.101:8080"
func (m *Map) Add(keys ... string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, key := range keys {
		// 为每个真实节点创建 replicas 个虚拟节点
		for i := 0; i < m.replicas; i++ {
			// 虚拟节点名称生成规则：i + key
			// 例如：0192.168.1.101:8080
			hash := int(m.hash([]byte(strconv.Itoa(i) + key)))

			// 将虚拟节点添加到环上
			m.keys = append(m.keys, hash)

			// 记录映射关系
			m.hashMap[hash] = key
		}
	}

	// 每次添加后，对哈希环进行排序，以便后续二分查找
	slices.Sort(m.keys)
}

// Get 根据数据 Key 选择最近的节点
func (m *Map) Get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.keys) == 0 {
		return ""
	}

	// 1. 计算数据 Key 的哈希值
	hash := int(m.hash([]byte(key)))

	// 2. 顺时针寻找第一个 >= hash 的虚拟节点
	// sort.Search 使用二分查找
	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})

	// 3. 如果 idx == len(m.keys), 说明环走到了尽头，回到起点
	if idx == len(m.keys) {
		idx = 0
	}

	// 4. 返回对应的真实节点
	return m.hashMap[m.keys[idx]]
}

func (m *Map) Remove(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 计算该节点所有的虚拟节点的 Hash 值，用于查找和删除
	hashesToRemove := make(map[int]bool)
	for i := 0; i < m.replicas; i++ {
		hash := int(m.hash([]byte(strconv.Itoa(i) + key)))
		hashesToRemove[hash] = true

		delete(m.hashMap, hash)
	}

	// 2. 重建 keys 切片（删除对应的 hash 值）
	// 使用原地过滤的技巧
	newKeys := m.keys[:0]
	for _, k := range m.keys {
		if !hashesToRemove[k] {
			newKeys = append(newKeys, k)
		}
	}
	m.keys = newKeys
}