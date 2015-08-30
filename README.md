pingbeat
========

*You know, for pings*

pingbeat sends ICMP pings to a list of targets and stores the round
trip time (RTT) in Elasticsearch (or elsewhere).  It uses
[tatsushid/go-fastping](https://github.com/tatsushid/go-fastping) for
sending/recieving ping packets and
[elastic/libbeat](https://github.com/elastic/libbeat) to talk to
Elasticsearch and other outputs.  Essentially, those two libraries do
all the heavy lifting, pingbeat is just glue around them.

## Installation

Install and update this go package with `go get -u
github.com/joshuar/pingbeat`

## Usage

See the [example configuration file](etc/pingbeat-example.yml) for configuring
your targets and assigning an output (default output is
Elasticsearch).

If using the Elasticsearch output, you should add a
new index template using the
[supplied one](etc/pingbeat-template.json), for example with `curl
-XPUT  /_template/pingbeat -d @/path/to/pingbeat-template.json`.

Once you've created a configuration file you can run
pingbeat with `pingbeat -c /path/to/pingbeat.yml`.

### Note on privileges

In order to send regular ICMP ping packets, pingbeat needs to open raw
sockets, which can only be done with superuser privileges.  So you
either need to run pingbeat with sudo or as root to send regular
pings. If you don't want to do that, set `privileged: false` in your
config and run pingbeat as a regular user.  It will then use a `UDP
ping` to test connectivity.

## License

pingbeat is licensed under the Apache 2.0 [license](LICENSE).
