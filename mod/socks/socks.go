package socks

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/things-go/go-socks5"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/net/proxy"
)

// // // // // // // // // //

var _ Interface = (*Obj)(nil)

// Obj — SOCKS5-прокси-сервер поверх Yggdrasil
type Obj struct {
	network  proxy.ContextDialer
	listener net.Listener
	addr     string
	isUnix   bool
	logger   yggcore.Logger
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// EnableConfigObj — параметры запуска SOCKS5
type EnableConfigObj struct {
	// Адрес: TCP "127.0.0.1:1080" или Unix "/tmp/ygg.sock"
	Addr string
	// Резолвер имён (.pk.ygg, DNS)
	Resolver socks5.NameResolver
	// Подробное логирование каждого соединения
	Verbose bool
	// Логгер; nil → без логирования
	Logger yggcore.Logger
	// Максимум одновременных соединений; 0 → без ограничений
	MaxConnections int
}

// New создаёт SOCKS-сервер (не запускает его)
func New(network proxy.ContextDialer) *Obj {
	return &Obj{network: network}
}

// //

// Enable запускает SOCKS5-прокси с указанными параметрами
func (s *Obj) Enable(cfg EnableConfigObj) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return fmt.Errorf("SOCKS already enabled on %s", s.addr)
	}

	opts := []socks5.Option{
		socks5.WithDial(s.network.DialContext),
	}
	if cfg.Resolver != nil {
		opts = append(opts, socks5.WithResolver(cfg.Resolver))
	}
	if cfg.Verbose {
		opts = append(opts, socks5.WithLogger(cfg.Logger))
	}
	server := socks5.NewServer(opts...)

	s.logger = cfg.Logger

	// Путь файловой системы → Unix-сокет, иначе TCP
	var err error
	if strings.HasPrefix(cfg.Addr, "/") || strings.HasPrefix(cfg.Addr, ".") {
		s.listener, err = listenUnix(cfg.Addr)
		s.isUnix = true
	} else {
		s.listener, err = net.Listen("tcp", cfg.Addr)
		s.isUnix = false
	}
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr, err)
	}
	s.addr = cfg.Addr

	if cfg.MaxConnections > 0 {
		s.listener = &limitedListenerObj{
			Listener: s.listener,
			sem:      make(chan struct{}, cfg.MaxConnections),
		}
	}

	s.logger.Infof("[socks] started on %s", cfg.Addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = server.Serve(s.listener)
	}()

	return nil
}

func (s *Obj) Disable() error {
	s.mu.Lock()
	if s.listener == nil {
		s.mu.Unlock()
		return nil
	}
	ln := s.listener
	isUnix := s.isUnix
	addr := s.addr
	s.listener = nil
	s.addr = ""
	s.isUnix = false
	s.mu.Unlock()

	err := ln.Close()
	if isUnix {
		_ = os.Remove(addr)
	}
	s.wg.Wait()

	s.logger.Infof("[socks] stopped on %s", addr)
	return err
}

// Addr — адрес прослушивания; пусто если не запущен
func (s *Obj) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *Obj) IsUnix() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isUnix
}

func (s *Obj) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener != nil
}

// //

// listenUnix открывает Unix-сокет с обработкой устаревших файлов
func listenUnix(path string) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if err == nil {
		return ln, nil
	}
	if !isAddrInUse(err) {
		return nil, err
	}
	// EADDRINUSE — проверяем, жив ли процесс-владелец
	probe, dialErr := net.Dial("unix", path)
	if dialErr == nil {
		_ = probe.Close()
		return nil, fmt.Errorf("another instance is listening on %q", path)
	}
	// Процесс мёртв — удаляем устаревший сокет и сразу слушаем
	if rmErr := removeUnixSocket(path); rmErr != nil {
		return nil, rmErr
	}
	return net.Listen("unix", path)
}

// removeUnixSocket — безопасное удаление; отказ при символической ссылке
func removeUnixSocket(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("os.Lstat %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to remove %s: is a symlink", path)
	}
	return os.Remove(path)
}

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.EADDRINUSE)
		}
	}
	return false
}

// //

// limitedListenerObj — семафор на число одновременных соединений
type limitedListenerObj struct {
	net.Listener
	sem chan struct{}
}

func (l *limitedListenerObj) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitedConnObj{Conn: conn, sem: l.sem}, nil
}

// limitedConnObj — соединение, освобождающее слот семафора при Close()
type limitedConnObj struct {
	net.Conn
	once sync.Once
	sem  chan struct{}
}

func (c *limitedConnObj) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.sem })
	return err
}
