BEATNAME=pingbeat
BEAT_DIR=github.com/joshuar
SYSTEM_TESTS=false
TEST_ENVIRONMENT=false
ES_BEATS=./vendor/github.com/elastic/beats
GOPACKAGES=$(shell glide novendor)
PREFIX?=.

# Path to the libbeat Makefile
-include $(ES_BEATS)/libbeat/scripts/Makefile

.PHONY: init
init:
	glide update  --no-recursive
	make update
	git init
	git add .

.PHONY: install-cfg
install-cfg:
	mkdir -p $(PREFIX)
	cp etc/pingbeat.template.json     $(PREFIX)/pingbeat.template.json
	cp etc/pingbeat.yml               $(PREFIX)/pingbeat.yml
	cp etc/pingbeat.yml               $(PREFIX)/pingbeat-linux.yml
	cp etc/pingbeat.yml               $(PREFIX)/pingbeat-binary.yml
	cp etc/pingbeat.yml               $(PREFIX)/pingbeat-darwin.yml
	cp etc/pingbeat.yml               $(PREFIX)/pingbeat-win.yml

.PHONY: update-deps
update-deps:
	glide update  --no-recursive
