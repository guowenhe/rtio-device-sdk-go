all: build

build:
	mkdir -p out
	go build -o ./out/ github.com/guowenhe/rtio-device-sdk-go/examples/...

clean:
	echo "clean all ..."
	go clean -i github.com/guowenhe/rtio-device-sdk-go/...
	rm -rf ./out 
	

deps:
	GO111MODULE=on go get -d -v github.com/guowenhe/rtio-device-sdk-go/...


.PHONY: \
	all \
	build \
	clean



