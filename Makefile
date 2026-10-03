GO_IMAGE ?= golang:1.25
DOCKER_GO = docker run --rm -v "$(CURDIR):/src" -v tollgate-gomod:/go/pkg/mod \
	-v tollgate-gobuild:/root/.cache/go-build -w /src $(GO_IMAGE)

.PHONY: test vet fmt-check tidy bench workload build run demo

test:
	$(DOCKER_GO) go test -race ./...

vet:
	$(DOCKER_GO) go vet ./...

fmt-check:
	$(DOCKER_GO) sh -c 'test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)'

tidy:
	$(DOCKER_GO) go mod tidy

workload:
	$(DOCKER_GO) go run ./bench/genworkload -out bench/workload.jsonl

bench:
	$(DOCKER_GO) go run ./bench -workload bench/workload.jsonl -out bench/results/latest.json

build:
	docker build -t tollgate:dev .

run: build
	docker run --rm -p $${TOLLGATE_PORT:-5450}:8787 -e TOLLGATE_DEMO_KEY \
		-v "$(CURDIR)/config.example.yaml:/etc/tollgate/tollgate.yaml:ro" tollgate:dev

demo:
	sh scripts/demo.sh
