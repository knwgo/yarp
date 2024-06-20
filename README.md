# YARP
*Yet Another Reverse Proxy*

A simple and easy-to-use reverse proxy that supports TCP, UDP, HTTP and HTTPS

### Usage
`./yarp -c {configFile}`

default config file path is `./yarp.toml`

### Config Example
```toml
[http]
bindAddr = "[::]:80"
    [[http.rules]]
    host = "example.com"
    target = "127.0.0.1:81"
    [[http.rules]]
    host = "another.example.com"
    target = "[fe80::88ef:c4ff:fe92:fa48]:81"
    [[http.rules]]
    host = "*.foo.bar"
    target = "127.0.0.1:80"

[https]
bindAddr = "0.0.0.0:443"
    [[https.rules]]
    host = "example.com"
    target = "127.0.0.1:444"

[[tcp]]
bindAddr = "[::]:4396"
target = "192.168.1.7:9527"
```
