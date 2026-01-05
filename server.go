package main

import (
	"io"
	"os"
	"net"
	"time"
	"fmt"
	"strings"
	"log"
	"context"
	"os/signal"
	"syscall"
)

var bufsiz int = 4 * 1048576


type data struct {
	Str []byte
	Size int
	Keep  bool
}

func doRunServe(version string, args []string) {
	opts, err := getRuntimeConf(version, args)
	if err != nil {
		fmt.Printf("%v", err)
		os.Exit(1)
	}

	debugPrint(log.Printf, levelWarning, "The current behaviour of archiving the ingested lines into the text file is deprecated, and the feature will be removed.\n")
	debugPrint(log.Printf, levelDebug, "Start serving\n")

	serve(opts)
}

/*
func serve(opts *Options) {

	debugPrint(log.Printf, levelCrazy, "Args=%v\n", opts)
	ch := make(chan data, 100)
	var conn net.Conn
	var err error

	history, err := NewHistory(opts.Cfg.Parser.TagsFile, opts.Cfg.HistoryFile)
	if err != nil {
		panic(err)
	}
	go cwdata(history, ch)
	ln, err := net.Listen("tcp", opts.Cfg.Server.ListnerClear.Addr)
	if err != nil {
		panic(err)
	}
	for {
		debugPrint(log.Printf, levelDebug, "Connection\n")
		conn, err = ln.Accept()
		if err != nil {
			panic( err)
			}
		go receivedata(conn, ch)
		}

	debugPrint(log.Printf, levelDebug, "exit\n")
	close(ch)
}
*/

func serve(opts *Options) {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", opts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ing, err := SetupIngestion(ctx, opts)
	if err != nil {
		log.Fatalf("failed to start ingestion: %v", err)
	}
	defer ing.Stop()

	waitForShutdown(cancel)

	debugPrint(log.Printf, levelDebug, "exit\n")
}

func waitForShutdown(cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	cancel()
}

func receivedata(rd net.Conn, ch chan data) {

	debugPrint(log.Printf, levelCrazy, "Args=%v, %v\n", rd, ch)
	defer 	debugPrint(log.Printf, levelCrazy, "dead\n")
	debugPrint(log.Printf, levelCrazy, "alive\n")
	b := make([]byte, bufsiz)

	for {
		_ = rd.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

		m, err := rd.Read(b)

		if m == 0 || err == io.EOF {
			break
			}
		debugPrint(log.Printf, levelCrazy, "sent 2 channel\n")
		ch <- data{Str:b, Size:m, Keep: true}
	}
	debugPrint(log.Printf, levelCrazy, "Connection closed\n")
	rd.Close()
}

func cwdata(h *History, ch chan data) {

	debugPrint(log.Printf, levelCrazy, "Args=%v, %v\n", h, ch)
	keep:=true
	defer 	debugPrint(log.Printf, levelCrazy, "dead\n")
	debugPrint(log.Printf, levelCrazy, "alive\n")

	for keep==true {
		debugPrint(log.Printf, levelCrazy, "Look Checkpoint1\n")
		b := <- ch
		debugPrint(log.Printf, levelCrazy, "Look Checkpoint2\n")
		cmd := string(b.Str[0:b.Size])
		h.SaveLog(strings.TrimSuffix(cmd, "\n"))
		h.ProcessCommand(strings.TrimSuffix(cmd, "\n"))
		debugPrint(log.Printf, levelCrazy, "Look Checkpoint3\n")
		keep=b.Keep;
	}

}
