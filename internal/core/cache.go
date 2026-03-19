package core

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"time"
)

const headerSize = 14

var ErrCacheMiss = errors.New("cache miss or hash collision")

type ZeroGCShard struct {
	// 索引字典：Key 是 64位 Hash，Value 是数据在 dataPool 中的起始偏移量 (Offset)
	// 绝对的 Zero-GC，内部不包含任何指针
	index map[uint64]uint32
	// 全局数据池：所有真实的 Key 和 Value 都序列化后追加到这里
	dataPool []byte
	// 当前已分配的内存偏移量尾部
	tail     uint32
	head     uint32
	capacity uint32 // 底层数组总容量
}

func NewZeroGCShard(capacity uint32) *ZeroGCShard {
	return &ZeroGCShard{
		index:    make(map[uint64]uint32),
		dataPool: make([]byte, capacity), // 预分配内存，减少运行时扩容开销
		tail:     0,
		head:     0,
		capacity: capacity,
	}
}

func (s *ZeroGCShard) Set(key string, value []byte, ttl time.Duration) {
	hashKey := hash(key)
	keyBytes := []byte(key)
	keyLen := uint16(len(keyBytes))
	valLen := uint32(len(value))
	// 计算这条数据需要的总字节数：
	// 2字节(存Key长度) + 4字节(存Value长度) + Key实际长度 + Value实际长度
	entryLen := headerSize + uint32(keyLen) + valLen
	if s.tail+entryLen > s.capacity {
		s.tail = 0 // 尾部回绕 (Wrap-around)
	}
	s.evict(entryLen)
	if ttl <= 0 {
		ttl = time.Hour * 24 * 365 // 默认1年，基本上相当于永不过期
	}
	expireAt := uint64(time.Now().Add(ttl).Unix())
	offset := s.tail
	binary.LittleEndian.PutUint64(s.dataPool[offset:offset+8], expireAt)
	binary.LittleEndian.PutUint16(s.dataPool[offset+8:offset+10], keyLen)
	binary.LittleEndian.PutUint32(s.dataPool[offset+10:offset+14], valLen)
	copy(s.dataPool[offset+14:], keyBytes)
	copy(s.dataPool[offset+14+uint32(keyLen):], value)
	s.index[hashKey] = offset
	s.tail += entryLen
}

func (s *ZeroGCShard) evict(needed uint32) {
	// 当 tail 逼近 head，且空间不足时，推着 head 往前走
	for s.capacity-s.tail < needed || (s.tail >= s.head && s.capacity-s.tail < needed) {
		if s.head == s.tail {
			break // 队列全空，无需驱逐
		}

		// 1. 读出 head 脚下老数据的 Header
		oldKeyLen := binary.LittleEndian.Uint16(s.dataPool[s.head+8 : s.head+10])
		oldValLen := binary.LittleEndian.Uint32(s.dataPool[s.head+10 : s.head+14])
		oldEntryLen := headerSize + uint32(oldKeyLen) + oldValLen

		// 2. 抽出老 Key，从 Map 中物理抹杀！
		oldKeyStart := s.head + headerSize
		oldKey := string(s.dataPool[oldKeyStart : oldKeyStart+uint32(oldKeyLen)])
		delete(s.index, hash(oldKey))

		// 3. head 指针无情地向前推进
		s.head += oldEntryLen
		if s.head == s.capacity {
			s.head = 0 // head 也需要回绕
		}
	}
}

func (s *ZeroGCShard) Delete(key string) {
	hashKey := hash(key)
	delete(s.index, hashKey)
}

func (s *ZeroGCShard) Get(key string) ([]byte, error) {
	hashKey := hash(key)
	offset, ok := s.index[hashKey]
	if !ok {
		return nil, ErrCacheMiss
	}
	expireAt := binary.LittleEndian.Uint64(s.dataPool[offset : offset+8])
	if uint64(time.Now().Unix()) > expireAt {
		return nil, ErrCacheMiss
	}
	keyLen := binary.LittleEndian.Uint16(s.dataPool[offset+8 : offset+10])
	valLen := binary.LittleEndian.Uint32(s.dataPool[offset+10 : offset+14])
	keyStart := offset + 14
	keyEnd := keyStart + uint32(keyLen)
	storedKey := string(s.dataPool[keyStart:keyEnd])
	if storedKey != key {
		return nil, ErrCacheMiss
	}
	valStart := keyEnd
	valEnd := valStart + valLen
	res := make([]byte, valLen)
	copy(res, s.dataPool[valStart:valEnd])
	return res, nil
}

// FNV-1a 64位高并发哈希算法 (计算速度极快)
func hash(key string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64()
}
