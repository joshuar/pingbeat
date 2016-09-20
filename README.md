pingbeat
========

*You know, for pings*

pingbeat sends ICMP pings to a list of targets and stores the round
trip time (RTT) in Elasticsearch (or elsewhere).  It uses
[elastic/libbeat](https://github.com/elastic/libbeat) to talk to
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

## Usage

See the [example configuration file](etc/beat.yml) for configuring
your targets and assigning an output (default output is
Elasticsearch).

There is a Kibana [export](etc/kibana/pingbeat.dashboard.json) you can use to
create some basic visulisations and a simple dashboard to explore
pingbeat data.

Once you've created a configuration file you can run
pingbeat with:

``` shell
pingbeat -c /path/to/beat.yml

```

To run Pingbeat with debugging output enabled, run:

``` shell
./pingbeat -c pingbeat.yml -e -d "*"

```

### Note on privileges

In order to send regular ICMP ping packets, pingbeat needs to open raw
sockets, which can only be done with superuser privileges.  So you
either need to run pingbeat with sudo or as root to send regular
pings. If you don't want to do that, set `privileged: false` in your
config and run pingbeat as a regular user.  It will then use a `UDP
ping` to test connectivity.  You may still need to adjust the
`net.ipv4.ping_group_range` sysctl variable to allow a regular user to
send UDP ping packets.

Alternatively, on Linux you can grant pingbeat access to raw sockets
without the need to run with sudo or as root user, by granting the
binary `CAP_NET_RAW ` capability (see:  [capabilities](http://linux.die.net/man/7/capabilities);
for overview of Linux capabilities). To set necessary capability,
ensure that the `getcap` and `setcap` binaries are present (you
might need to install relevant packages from you distribution) in
your `PATH`, then execute the following: `setcap cap_net_raw+ep <PATH>`
where `PATH` is the location where the pingbeat binary was installed;
to verify execute the following: `getcap <PATH>`. Given that everything
went well, the output from `getcap` should indicate that pingbeat has
now `cap_net_raw+ep` capabilities set.

## License

pingbeat is licensed under the Apache 2.0 [license](LICENSE).
