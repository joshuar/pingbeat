#
# spec file for package pingbeat
#
# Copyright (c) 2015 Joshua Rich joshua.rich@gmail.com
#
# All modifications and additions to the file contributed by third parties
# remain the property of their copyright owners, unless otherwise agreed
# upon. The license for this file, and modifications and additions to the
# file, is the same license as for the pristine package itself (unless the
# license for the pristine package is not an Open Source License, in which
# case the license is the MIT License). An "Open Source License" is a
# license that conforms to the Open Source Definition (Version 1.9)
# published by the Open Source Initiative.

# Please submit bugfixes or comments via http://bugs.opensuse.org/
#

%global commit      0a24160d5b609fbc31f735295b2a7787797ecabf
%global shortcommit %(c=%{commit}; echo ${c:0:7})

Name:		pingbeat
Version:	0.0.2
Release:        1%{?dist}
Summary:	Pingbeat sends ICMP packets and stores the RTT in Elasticsearch

License:	Apache 2.0
URL:		https://github.com/joshuar/pingbeat
Source0:	https://github.com/joshuar/pingbeat/archive/v0.0.2.tar.gz

BuildRequires:  golang >= 1.6.2
BuildRequires: 	systemd

%description
pingbeat sends ICMP pings to a list of targets and stores the round
trip time (RTT) in Elasticsearch (or elsewhere). It uses
tatsushid/go-fastping for sending/recieving ping packets and
elastic/libbeat to talk to Elasticsearch and other
outputs. Essentially, those two libraries do all the heavy lifting,
pingbeat is just glue around them.

%prep
%setup -q -c pingbeat-%{version}

%build
make glide
./glide install --update-vendored
mkdir -p ./_build
ln -s $(pwd)/vendor ./_build/src
GOPATH=$(pwd)/_build:%{gopath} go build -o pingbeat .

%install
install -d %{buildroot}%{_bindir}
install -p -m 0755 ./pingbeat %{buildroot}%{_bindir}/pingbeat
install -d %{buildroot}%{_sysconfdir}/pingbeat
install -p -m 0644 ./etc/beat.yml %{buildroot}%{_sysconfdir}/pingbeat/pingbeat.yml
install -p -m 0644 ./etc/pingbeat.template.json %{buildroot}%{_sysconfdir}/pingbeat/pingbeat.template.json
install -d %{buildroot}%{_unitdir}
install -p -m 0644 ./etc/pingbeat.service %{buildroot}%{_unitdir}/pingbeat.service

%post
%systemd_post pingbeat.service

%preun
%systemd_preun pingbeat.service

%postun
%systemd_postun_with_restart pingbeat.service

%files
%defattr(-,root,root,-)
%doc CONTRIBUTING.md LICENSE README.md
%{_bindir}/pingbeat
%{_sysconfdir}/pingbeat

%changelog
* Sun Jul 10 2016 Joshua Rich <joshua.rich@gmail.com>
-
