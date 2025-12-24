.PHONY: build
build: 
	CGO_ENABLED=0 go build -o cdtool-linux-amd64
	docker build . -t ${IMG}
	docker push ${IMG}
