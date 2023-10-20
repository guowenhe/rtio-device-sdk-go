all: build

build:
	mkdir -p out
	go build -o ./out/ rtio2/examples/...

clean:
	echo "clean all ..."
	go clean -i rtio2/...
	rm -rf ./out 
	

deps:
	GO111MODULE=on go get -d -v rtio2/...


.PHONY: \
	all \
	build \
	clean



