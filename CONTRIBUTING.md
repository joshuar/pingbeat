### Init Project

Pingbeat uses [glide](https://github.com/Masterminds/glide) for
managing dependancies. You'll need to install glide to help with development.

To get running with Pingbeat, run the following commands:

```
glide update --no-recursive
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

The usual go mantra works.  E.g.:

```
go build
```

### Install

Because Pingbeat uses glide, so you should use:

```
glide install
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
