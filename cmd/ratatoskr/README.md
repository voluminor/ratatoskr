# ratatoskr

## Build

```bash
go build -ldflags="-s -w" -trimpath -o ratatoskr .
```

## Info & help

```bash
./ratatoskr -i
```

```bash
./ratatoskr -h
```

## Key utilities

### Generate vanity key

```bash
./ratatoskr -go.keys.gen 10s
```

### Show address for a key

```bash
./ratatoskr -go.keys.addr <hex-private-128|hex-public-64|path-to-pem>
```

### Convert hex key to PEM

```bash
./ratatoskr -go.keys.to_pem key.pem -go.keys.addr <hex-private-128>
```

### Convert PEM to hex key

```bash
./ratatoskr -go.keys.from_pem key.pem
```

## Configuration utilities

### Generate default config

```bash
./ratatoskr -go.conf.generate.path /etc/ratatoskr
```

```bash
./ratatoskr -go.conf.generate.path /etc/ratatoskr -go.conf.generate.preset medium
```

```bash
./ratatoskr -go.conf.generate.path /etc/ratatoskr -go.conf.generate.preset full -go.conf.generate.format json
```

### Import Yggdrasil config

```bash
./ratatoskr -go.conf.import.from /etc/yggdrasil/yggdrasil.conf -go.conf.import.to /etc/ratatoskr
```

### Export to Yggdrasil config

```bash
./ratatoskr -go.conf.export.from /etc/ratatoskr/ratatoskr-config.yml -go.conf.export.to /etc/yggdrasil
```

## Peer info

```bash
./ratatoskr -go.peer_info.url tcp://1.2.3.4:5678,tcp://5.6.7.8:1234
```

```bash
./ratatoskr -go.peer_info.url tcp://1.2.3.4:5678 -go.peer_info.format json
```

## Port forwarding

```bash
./ratatoskr -go.forward.from 127.0.0.1:8080 -go.forward.to [200:abc::1]:8080 -go.forward.peer tcp://1.2.3.4:5678
```

```bash
./ratatoskr -go.forward.from 127.0.0.1:5353 -go.forward.to [200:abc::1]:53 -go.forward.proto udp -go.forward.peer tcp://1.2.3.4:5678
```

## Traceroute

```bash
./ratatoskr -go.traceroute.trace a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924 -go.traceroute.peer tcp://yggdrasil.sunsung.fun:4442
```

```bash
./ratatoskr -go.traceroute.scan -go.traceroute.peer tcp://yggdrasil.sunsung.fun:4442
```

### Ping

```bash
./ratatoskr -go.traceroute.ping a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924 -go.traceroute.peer tcp://yggdrasil.sunsung.fun:4442
```

```bash
./ratatoskr -go.traceroute.ping a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924 -go.traceroute.peer tcp://yggdrasil.sunsung.fun:4442 -go.traceroute.count 10
```
