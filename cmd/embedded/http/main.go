package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	golog "github.com/gologme/log"
	qrcode "github.com/skip2/go-qrcode"
	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr"
	"github.com/voluminor/ratatoskr/mod/core"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/traceroute"
)

// // // // // // // // // //

const shutdownTimeout = 2 * time.Second

// //

func main() {
	wwwPath := flag.String("www", "www", "path to the www directory")
	cfgPath := flag.String("config", "conf.yml", "path to the config file")
	flag.Parse()

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Println("Error: load config:", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	nodeCfg := yggconfig.GenerateConfig()
	nodeCfg.AdminListen = "none"
	if cfg.PrivateKey != "" {
		key, err := hex.DecodeString(cfg.PrivateKey)
		if err != nil || len(key) != 64 {
			fmt.Println("Error: invalid private_key — must be a 128-char hex string (64 bytes)")
			os.Exit(1)
		}
		nodeCfg.PrivateKey = key
	}

	logger := golog.New(os.Stdout, "", golog.LstdFlags)
	logger.EnableLevel("info")
	logger.EnableLevel("warn")
	logger.EnableLevel("error")

	node, err := ratatoskr.New(ratatoskr.ConfigObj{
		Ctx:             ctx,
		Config:          nodeCfg,
		CoreStopTimeout: shutdownTimeout,
		Logger:          logger,
		Peers: &peermgr.ConfigObj{
			Peers:     cfg.Peers,
			BatchSize: 4,
		},
	})
	if err != nil {
		fmt.Println("Error: start yggdrasil:", err)
		os.Exit(1)
	}
	defer node.Close()

	if cfg.PrivateKey == "" {
		logger.Warnf("auto-generated private key (add to conf.yml to keep the same address across restarts):")
		logger.Warnf("private_key: %s", hex.EncodeToString(nodeCfg.PrivateKey))
	}

	coreNode := node.Interface.(*core.Obj)
	tr, err := traceroute.New(coreNode.UnsafeCore(), logger)
	if err != nil {
		fmt.Println("Error: traceroute:", err)
		os.Exit(1)
	}

	info := newInfoHandler(node, tr, cfg, logger)
	traceHandler := newTraceHandler(tr)
	treeHandler := newTreeHandler(tr)

	yggAddr := node.Address().String()
	qrURL := fmt.Sprintf("http://[%s]:%d/", yggAddr, cfg.YggPorts[0])
	qrHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		png, err := qrcode.Encode(qrURL, qrcode.Medium, 256)
		if err != nil {
			http.Error(w, "qr error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})

	// Plain HTTP servers
	for _, port := range cfg.HTTPPorts {
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			fmt.Printf("Error: listen HTTP :%d: %v\n", port, err)
			os.Exit(1)
		}
		go (&http.Server{
			Handler:           buildMux(*wwwPath, info, false, qrHandler, traceHandler, treeHandler),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		}).Serve(l)
		fmt.Printf("HTTP       http://%s:%d/\n", cfg.Hostname, port)
	}

	// Yggdrasil HTTP servers
	for _, port := range cfg.YggPorts {
		addr := fmt.Sprintf("[%s]:%d", yggAddr, port)
		l, err := node.Listen("tcp", addr)
		if err != nil {
			fmt.Printf("Error: listen Yggdrasil :%d: %v\n", port, err)
			os.Exit(1)
		}
		go (&http.Server{
			Handler:           buildMux(*wwwPath, info, true, qrHandler, traceHandler, treeHandler),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		}).Serve(l)
		fmt.Printf("Yggdrasil  http://[%s]:%d/\n", yggAddr, port)
	}

	<-ctx.Done()
}

// //

func buildMux(wwwPath string, info *InfoHandlerObj, isYgg bool, qr, trace, tree http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/yggdrasil-server.json", info.Handler(isYgg))
	mux.Handle("/ygg-qr.png", qr)
	mux.Handle("/traceroute.json", trace)
	mux.Handle("/tree.json", tree)
	if isYgg {
		mux.Handle("/", newYggFileHandler(wwwPath))
	} else {
		mux.Handle("/", newPlainFileHandler(wwwPath))
	}
	return mux
}
