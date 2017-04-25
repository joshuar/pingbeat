BEAT_NAME=pingbeat
BEAT_PATH=github.com/joshuar/pingbeat
BEAT_GOPATH=$(firstword $(subst :, ,${GOPATH}))
BEAT_URL=https://${BEAT_PATH}
SYSTEM_TESTS=false
TEST_ENVIRONMENT=false
ES_BEATS?=./vendor/github.com/elastic/beats
GOPACKAGES=$(shell glide novendor)
PREFIX?=.
NOTICE_FILE=NOTICE
GLIDE_VERSION=0.12.3

# Path to the libbeat Makefile
-include $(ES_BEATS)/libbeat/scripts/Makefile

# Initial beat setup
.PHONY: setup
setup: copy-vendor
	make update

# Copy beats into vendor directory
.PHONY: copy-vendor
copy-vendor:
	glide install

.PHONY: git-init
git-init:
	git init
	git add README.md CONTRIBUTING.md
	git commit -m "Initial commit"
	git add LICENSE
	git commit -m "Add the LICENSE"
	git add .gitignore
	git commit -m "Add git settings"
	git add .
	git reset -- .travis.yml
	git commit -m "Add pingbeat"
	git add .travis.yml
	git commit -m "Add Travis CI"

# This is called by the beats packer before building starts
.PHONY: before-build
before-build:

# Collects all dependencies and then calls update
.PHONY: collect
collect:

.PHONY: cross
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
