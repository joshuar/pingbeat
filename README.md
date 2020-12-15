
# DEPRECATED 
[![No Maintenance Intended](http://unmaintained.tech/badge.svg)](http://unmaintained.tech/) This project is unmaintained and abandoned.  Check out [Heartbeat](https://www.elastic.co/beats/heartbeat) instead.


pingbeat
========

*You know, for pings*

pingbeat sends ICMP pings to a list of targets and stores the round
trip time (RTT) in Elasticsearch (or elsewhere).  It uses
[elastic/beats/libbeat](https://github.com/elastic/beats/tree/master/libbeat) to talk to
Elasticsearch and other outputs.

## Requirements

pingbeat has the same requirements around the Go environment as
libbeat, see
[here](https://github.com/elastic/beats/blob/master/CONTRIBUTING.md#dependencies).

## Installation

Install and configure [Go](https://golang.org/doc/install).

Clone this repo:

``` shell
git clone git@github.com:joshuar/pingbeat.git
```

Run `make install` in the repo directory.

The `pingbeat` binary will then be available in `$GOPATH/bin`.

If intending on using the Elasticsearch output, you should add a
new index template using the
[supplied one](etc/pingbeat.template.json), for example with:

``` shell
curl -XPUT  /_template/pingbeat -d @/path/to/pingbeat.template.json

```

## Documentation

See the [documentation here](docs/index.asciidoc)

## License

pingbeat is licensed under the Apache 2.0 [license](LICENSE).
