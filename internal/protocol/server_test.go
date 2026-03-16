package protocol

import (
	"Flux-KV/internal/config"
	"Flux-KV/internal/core"
	"net"
	"testing"
	"time"
)

// TestServer_EndToEnd 是一个端到端的集成测试
// 它模拟了：启动服务器 -> 客户端连接 -> 发送命令 -> 接收响应 -> 验证结果
func TestServer_EndToEnd(t *testing.T) {
	// 1. 初始化依赖（Core MemDB）
	// 测试不需要 AOF，给个空配置即可
	cfg := &config.Config{}
	db, _ := core.NewMemDB(cfg)

	// 2. 创建 Server
	addr := "localhost:9090"
	server := NewServer(addr, db)

	// 3. 在独立的 Goroutine 中启动服务
	go func() {
		if err := server.Start(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()

	// 给服务器时间启动
	time.Sleep(100 * time.Millisecond)

	// 4. Client 模拟：连接服务器
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer conn.Close()

	// 5. 定义测试用例（Table-Driven Tests）
	tests := []struct {
		name     string // 用例名称
		cmd      string // 发送的命令
		expected string // 期望收到的响应
	}{
		{"SetString", "SET name naato", "OK"},
		{"GetString", "GET name", "naato"},
		{"SetNumber", "SET age 18", "OK"},
		{"GetNumber", "GET age", "18"}, // 注意：虽然存进去是字符串，但为了验证格式
		{"Update", "SET name go-expert", "OK"},
		{"GetUpdate", "GET name", "go-expert"},
		{"Delete", "DEL name", "OK"},
		{"GetDeleted", "GET name", "(nil)"},
	}

	// 6. 循环执行测试用例
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A. 编码请求
			reqData, err := Encode(tt.cmd)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			// B. 发送请求
			_, err = conn.Write(reqData)
			if err != nil {
				t.Fatalf("Write failed: %v", err)
			}

			// C. 解码响应
			resp, err := Decode(conn)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			// D. 断言验证
			if resp != tt.expected {
				t.Errorf("Command: %q, Expected: %q, Got: %q", tt.cmd, tt.expected, resp)
			}
		})
	}
}
