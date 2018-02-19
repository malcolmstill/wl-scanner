wl-scanner
==========

The `wl-scanner` project is designed parse and generate Go client code
from a wayland protocol file.

In its base form, it produces the `client.go` code in
`github.com/dkolbly/wl` from the canonical definition of the wayland
protocol at
https://cgit.freedesktop.org/wayland/wayland/plain/protocol/wayland.xml

It is similar in concept to the wayland-scanner tool which was
developed for generating the C client library.

This is a hobby project intended to help people understand the
wayland.xml protocol file and the generation go code, as well as to
help build client libraries for protocols around Wayland.

## Usage

```
go get github.com/dkolbly/wl-scanner

# generate a client for the base protocol
wl-scanner -source https://cgit.freedesktop.org/wayland/wayland/plain/protocol/wayland.xml \
        -output $GOPATH/src/github.com/dkolbly/wl/client.go

# generate a client for the xdg-shell protocol
wl-scanner -source https://raw.githubusercontent.com/wayland-project/wayland-protocols/master/stable/xdg-shell/xdg-shell.xml \
        -output $GOPATH/src/github.com/dkolbly/wl/xdg-shell.go
```
