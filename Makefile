.PHONY: build
build: 
	CGO_ENABLED=0 go build -o cdtool-linux-amd64


.PHONY: build-img
build-img: 
	docker build . -t ${IMG}
	docker push ${IMG}
	docker buildx imagetools create --tag ${IMGNOTAG}:latest ${IMG}