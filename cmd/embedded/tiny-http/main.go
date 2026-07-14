package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr"
)

// // // // // // // // // //

const (
	peer            = "tls://yggdrasil.sunsung.fun:4443"
	port            = 8080
	shutdownTimeout = 5 * time.Second
)

// //

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Plain TCP server
	tcpListener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Println("Error: listen TCP:", err)
		return
	}
	tcpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, "hello from the network")
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() { _ = tcpServer.Serve(tcpListener) }()
	defer func() { _ = tcpServer.Close() }()
	fmt.Printf("HTTP    http://localhost:%d\n", port)

	// //

	// Yggdrasil server
	cfg := yggconfig.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.Peers = []string{peer}

	node, err := ratatoskr.New(ratatoskr.ConfigObj{Ctx: ctx, Config: cfg, CloseTimeout: shutdownTimeout})
	if err != nil {
		fmt.Println("Error: start yggdrasil:", err)
		return
	}
	defer func() { _ = node.Close() }()

	yggAddr := fmt.Sprintf("[%s]:%d", node.Address(), port)
	yggListener, err := node.Listen("tcp", yggAddr)
	if err != nil {
		fmt.Println("Error: listen yggdrasil:", err)
		return
	}
	yggServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, "hello from the Yggdrasil network")
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() { _ = yggServer.Serve(yggListener) }()
	defer func() { _ = yggServer.Close() }()
	fmt.Printf("Yggdrasil http://[%s]:%d\n", node.Address(), port)

	// //

	<-ctx.Done()
}
