### Init Project
To get running with Pingbeat, run the following commands:

```
glide update --no-recursive
make update

```


To push Pingbeat in the git repository, run the following commands:

```
git init
git add .
git commit
git remote set-url origin https://github.com/joshuar/pingbeat
git push origin master

```

For further development, check out the
[beat developer guide](https://www.elastic.co/guide/en/beats/libbeat/current/new-beat.html).

### Build

To build the binary for Pingbeat run the command below. This will
generate a binary
in the same directory with the name pingbeat.

```
make

```

### Test

To test Pingbeat, run the following commands:

```
make testsuite

```

alternatively:

```
make unit-tests
make system-tests
make integration-tests
make coverage-report

```

The test coverage is reported in the folder `./build/coverage/`


### Update

Each beat has a template for the mapping in elasticsearch and a
documentation for the fields
which is automatically generated based on `etc/fields.yml`.
To generate etc/pingbeat.template.json and etc/pingbeat.asciidoc

```
make update

```


### Cleanup

To clean  Pingbeat source code, run the following commands:

```
make fmt
make simplify

```

To clean up the build directory and generated artifacts, run:

```
make clean

```


### Clone

To clone Pingbeat from the git repository, run the following commands:

```
mkdir -p ${GOPATH}/github.com/joshuar
cd ${GOPATH}/github.com/joshuar
git clone https://github.com/joshuar/pingbeat

```

For further development, check out the
[beat developer guide](https://www.elastic.co/guide/en/beats/libbeat/current/new-beat.html).
