GLIDE_VERSION=0.11.0

all: deps test
	go build

deps: glide
	./glide install

glide:
ifeq ($(shell uname),Darwin)
	curl -L https://github.com/Masterminds/glide/releases/download/v$(GLIDE_VERSION)/glide-v$(GLIDE_VERSION)-darwin-amd64.zip -o glide.zip
	unzip glide.zip
	mv ./darwin-amd64/glide ./glide
	rm -fr ./darwin-amd64
	rm ./glide.zip
else
	curl -L https://github.com/Masterminds/glide/releases/download/v$(GLIDE_VERSION)/glide-v$(GLIDE_VERSION)-linux-amd64.zip -o glide.zip
	unzip glide.zip
	mv ./linux-amd64/glide ./glide
	rm -fr ./linux-amd64
	rm ./glide.zip
endif

cross:
	test -d dist || mkdir dist
	echo "Building Windows 32-bit..."
	env GOOS=windows GOARCH=386 go build -o dist/pingbeat-windows-32.exe
	echo "Building Windows 64-bit..."
	env GOOS=windows GOARCH=amd64 go build -o dist/pingbeat-windows-64.exe
	echo "Building Linux 32-bit..."
	env GOOS=linux GOARCH=386 go build -o dist/pingbeat-linux-32.exe
	echo "Building Linux 64-bit..."
	env GOOS=linux GOARCH=amd64 go build -o dist/pingbeat-linux-64.exe
	echo "Building OSX 32-bit..."
	env GOOS=darwin GOARCH=386 go build -o dist/pingbeat-darwin-32.exe
	echo "Building OSX 64-bit..."
	env GOOS=darwin GOARCH=amd64 go build -o dist/pingbeat-darwin-64.exe

test:
	go test $(glide novendor)

clean:
	rm ./glide
	rm dist/pingbeat-*

install: deps test
	go install

.PHONY: all test clean glide install cross
