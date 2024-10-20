.PHONY: build

build:
	mkdir -p build

	GOOS=linux GOARCH=amd64 go build -o build/kube-power_linux_amd64
	GOOS=linux GOARCH=arm64 go build -o build/kube-power_linux_arm64
	GOOS=darwin GOARCH=amd64 go build -o build/kube-power_darwin_amd64
	GOOS=darwin GOARCH=amd64 go build -o build/kube-power_darwin_arm64

build-docker:
	docker build -t kube-power .

clean:
	go clean
	rm -rf build

test:
	go test -race -v ./...

vet:
	go vet

lint:
	golangci-lint run
