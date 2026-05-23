package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"Flux-KV/internal/config"
	"Flux-KV/pkg/logger"
	"Flux-KV/pkg/network/client"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func main() {
	flag.Parse()

	config.InitConfig()
	logger.InitLogger()
	defer logger.Log.Sync()

	log := logger.Log

	// 直接连接 Gateway gRPC
	gatewayAddr := viper.GetString("gateway.address")
	if gatewayAddr == "" {
		gatewayAddr = "localhost:50051"
	}
	log.Info("Connecting to Gateway...", zap.String("addr", gatewayAddr))

	conn, err := client.NewDirectConn(gatewayAddr)
	if err != nil {
		log.Fatal("Failed to connect to Gateway", zap.Error(err))
	}
	defer conn.Close()
	kvClient := client.NewClient(conn)

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║  Flux-KV gRPC Client Ready                  ║")
	fmt.Println("║                                              ║")
	fmt.Println("║  SET <key> <value>  [ap|cp]  Write key-value ║")
	fmt.Println("║  GET <key>          [ap|cp]  Read key-value  ║")
	fmt.Println("║  DEL <key>          [ap|cp]  Delete key      ║")
	fmt.Println("║  EXIT                        Exit client     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Flux-KV> ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		parts := strings.Fields(text)
		cmd := strings.ToUpper(parts[0])

		if cmd == "EXIT" || cmd == "QUIT" {
			fmt.Println("Bye!")
			break
		}

		if len(parts) < 2 {
			fmt.Println("Usage: SET|GET|DEL <key> [value] [mode]")
			continue
		}

		key := parts[1]
		mode := "ap"
		if len(parts) >= 3 {
			m := strings.ToLower(parts[len(parts)-1])
			if m == "cp" {
				mode = "cp"
			}
		}

		switch cmd {
		case "SET":
			if len(parts) < 3 {
				fmt.Println("Usage: SET <key> <value> [ap|cp]")
				continue
			}
			valIdx := 2
			valEnd := len(parts)
			if len(parts) >= 4 && strings.ToLower(parts[len(parts)-1]) == "cp" {
				mode = "cp"
				valEnd = len(parts) - 1
			}
			value := strings.Join(parts[valIdx:valEnd], " ")
			if err := kvClient.SetWithMode(context.Background(), key, value, mode); err != nil {
				fmt.Printf("SET error: %v\n", err)
			} else {
				fmt.Println("OK")
			}

		case "GET":
			val, err := kvClient.GetWithMode(context.Background(), key, mode)
			if err != nil {
				fmt.Printf("GET error: %v\n", err)
			} else if val == "" {
				fmt.Println("(nil)")
			} else {
				fmt.Printf("\"%s\"\n", val)
			}

		case "DEL":
			if err := kvClient.DelWithMode(context.Background(), key, mode); err != nil {
				fmt.Printf("DEL error: %v\n", err)
			} else {
				fmt.Println("(integer) 1")
			}

		default:
			fmt.Printf("Unknown command: %s\n", cmd)
		}
	}
}
