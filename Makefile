

build/ab-mock-server: cmd/mock-server/main.go pkg/mock/server.go
	go build -o build/ab-mock-server ./cmd/mock-server/

.PHONY: bench
bench: build/ab-mock-server
	ulimit -n 10240 && ./build/ab-mock-server 127.0.0.1:8123 &
	ulimit -n 10240 && ABSMARTLY_ENDPOINT='http://127.0.0.1:8123' go test -run XXX -bench=. \
		-cpu 1 -benchmem -benchtime 1s \
		-cpuprofile build/cpu.out -memprofile build/mem.out -mutexprofile build/mutex.out \
		-o build/benchmark.test ./pkg/benchmark/
	pkill ab-mock-server
