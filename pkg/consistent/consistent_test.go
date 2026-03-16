package consistent

import (
	"fmt"
	"strconv"
	"testing"
)

func TestHashing(t *testing.T) {
	// 1. 初始化：虚拟节点倍数为 50
	hash := New(200, nil)

	// 2. 添加 3 个物理节点
	// 假设是三个 redis 节点
	hash.Add("nodeA", "nodeB", "nodeC")

	// 3. 模拟十万个 Key，统计分布
	testCases := make(map[string]int)

	for i := 0; i < 100_000; i++ {
		key := "key_" + strconv.Itoa(i)
		node := hash.Get(key)
		testCases[node]++
	}

	// 4. 打印结果
	fmt.Println("--- Distribution Check (100k keys) ---")
	for node, count := range testCases {
		fmt.Printf("%s: %d (%.2f%%)\n", node, count, float64(count)/100000.0*100)
	}
}

// 测试添加节点后的一致性（缓存是否还要迁移）
func TestConsistency(t *testing.T) {
	hash := New(200, nil)
	hash.Add("nodeA", "nodeB", "nodeC")

	// 记录 key_12345 原本属于谁
	originNode := hash.Get("key_12345")
	fmt.Printf("Before Add: key_12345 -> %s\n", originNode)

	// 添加新结点 nodeD
	hash.Add("nodeD")

	// 再次查看
	newNode := hash.Get("key_12345")
	fmt.Printf("After Add: key_12345 -> %s\n", newNode)
	
	// 这里大概率不变，或者变成 nodeD，但不应该变成随意的其他旧节点
}

func TestRemove(t *testing.T) {
	hash := New(50, nil)
	// 初始：A, B, C
	hash.Add("nodeA", "nodeB", "nodeC")

	// 假设 key_remove_test 原本落在 nodeB
	// 我们不断找，直到找到一个落在 nodeB 的 key (为了测试效果)
	targetKey := "key_remove_test" 
	// 注意：如果运气不好 key 不属于 nodeB，这个测试逻辑可能要微调，
	// 但为了演示，我们先假设它落在我们要删的节点上，或者我们打印出来看看。
	
	initialNode := hash.Get(targetKey)
	fmt.Printf("Key '%s' initially mapped to: %s\n", targetKey, initialNode)

	// 移除该节点
	hash.Remove(initialNode)

	// 再次获取
	newNode := hash.Get(targetKey)
	fmt.Printf("Key '%s' now mapped to: %s\n", targetKey, newNode)

	if initialNode == newNode {
		t.Errorf("Node should have changed after removal, but still %s", newNode)
	}
}