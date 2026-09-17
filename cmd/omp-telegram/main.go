package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"omp-telegram/internal/bridge"
	"omp-telegram/internal/config"
	"omp-telegram/internal/store"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "", "TOML configuration file (--config or -c; default: executable-adjacent config.toml, otherwise embedded defaults)")
	flag.StringVar(path, "c", "", "short form of --config")
	check := flag.Bool("check", false, "validate configuration without connecting (--check)")
	flag.Parse()
	c, e := config.Load(*path)
	if e != nil {
		return e
	}
	if *check {
		fmt.Println("configuration valid")
		return nil
	}
	syscall.Umask(0077)
	lock, e := os.OpenFile(filepath.Join(c.DataDir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fmt.Errorf("data directory already locked")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	db, e := store.Open(c.DataDir)
	if e != nil {
		return e
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return bridge.Run(ctx, c, db)
}
