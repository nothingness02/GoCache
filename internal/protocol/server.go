package protocol

import (
	"Flux-KV/internal/core"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
)

type Server struct {
	addr string
	store *core.MemDB	// 关联内存数据库实例
}

func NewServer(addr string, store *core.MemDB) *Server {
	return &Server{
		addr: addr,
		store: store,
	}
}

// Start 启动服务
func (s *Server) Start() error {
	// 1. 启动TCP监听
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Printf("🚀 TCP Server listening on %s", s.addr)

	// 2. 死循环接受客户端连接（核心）
	for {
		conn, err := listener.Accept()	// 阻塞等待新连接
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		// 3. 并发处理：每个连接启动独立Goroutine
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()	// 连接处理完后关闭，释放资源

	clientAddr := conn.RemoteAddr().String()
	log.Printf("New connection from: %s", clientAddr)

	for {
		// 1. 拆包：读取完整请求（解决TCP粘包）
		request, err := Decode(conn)
		if err != nil {
			if err == io.EOF {
				// 客户端主动断开连接
				log.Printf("Client %s disconnected", clientAddr)
			} else {
				log.Printf("Read error: %v", err)
			}
			return	// 退出循环，结束当前连接的处理
		}

		// 🔍 观察点 4: 服务端收到了完整的数据包
        fmt.Printf("[Server] 3. 收到并拆包成功: %q\n", request)

		// 2. 执行命令：解析并操作数据库
		response := s.executeCommand(request)

		// 🔍 观察点 5: 数据库操作完成，准备回复
        fmt.Printf("[Server] 4. 执行完毕，结果: %q. 准备发回客户端...\n", response)
		
		// 3. 打包+发送响应
		responseData, err := Encode(response)
		if err != nil {
			log.Printf("Encode error: %v", err)
			return
		}

		_, err = conn.Write(responseData)
		if err != nil {
			log.Printf("Write error: %v", err)
			return
		}
	}
}

// executeCommand 解析简单的文本协议
func (s *Server) executeCommand(cmdStr string) string {
	// 清理空格并按空格分割命令
	parts := strings.Fields(strings.TrimSpace(cmdStr))
	if len(parts) == 0 {
		return "ERROR: Empty command"
	}

	cmd := strings.ToUpper(parts[0])

	switch cmd {
	case "SET":
		if len(parts) < 3 {
			// 参数校验：SET需要key+value
			return "ERROR: SET requires key and value"
		}
		s.store.Set(parts[1], parts[2], 0)
		return "OK"
	case "GET":
		if len(parts) < 2 {
			// 参数校验：GET需要key
			return "ERROR: GET requires key"
		}
		val, found := s.store.Get(parts[1])
		if !found {
			return "(nil)"	// 模仿Redis的返回格式
		}
		return fmt.Sprintf("%v", val)
	case "DEL":
		if len(parts) < 2 {
			// 参数校验：DEL需要key
			return "ERROR: DEL requires key"
		}
		s.store.Del(parts[1])
		return "OK"
	default:
		return fmt.Sprintf("ERROR: Unknown command '%s'", cmd)
	}
}