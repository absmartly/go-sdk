package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/absmartly/go-sdk/pkg/mock"
)

func main() {
	if len(os.Args) != 2 {
		log.Printf("Usage:\n%s <listen-address>\n\nExamples:\n%s :8080\n%s 127.0.0.1",
			os.Args[0], os.Args[0], os.Args[0])
		return
	}
	listen := os.Args[1]

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-done
		cancel()
	}()

	ms := mock.NewServerMock(listen)
	log.Printf("Starting server on '%s'", listen)
	err := ms.Run(ctx)
	log.Println(err)
}
