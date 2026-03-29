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
./ratatoskr -go.key.gen 10s
```

### Show address for a key

```bash
./ratatoskr -go.key.addr <hex-private-128|hex-public-64|path-to-pem>
```

### Convert hex key to PEM

```bash
./ratatoskr -go.key.to_pem key.pem -go.key.addr 5623515376dcafe397e79de5a5ba125adc71beb36f9659a189ee7cb8640855580000278fa1ad5448ce0217a6174f7894acdd921d1021e05758da518aaf73ad80
```

### Convert PEM to hex key

```bash
./ratatoskr -go.key.from_pem key.pem
```

## Configuration utilities

### Generate default config

```bash
./ratatoskr -go.conf.generate.path ./
```

```bash
./ratatoskr -go.conf.generate.path ./ -go.conf.generate.preset medium
```

```bash
./ratatoskr -go.conf.generate.path ./ -go.conf.generate.preset full -go.conf.generate.format conf
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
./ratatoskr -go.peer_info.url tcp://yggdrasil.sunsung.fun:4442,quic://yggdrasil.sunsung.fun:4441
```

```bash
./ratatoskr -go.peer_info.url tcp://yggdrasil.sunsung.fun:4442 -go.peer_info.format json
```

## Port forwarding

```bash
./ratatoskr -go.forward.from 127.0.0.1:8080 -go.forward.to [200:b0aa:c535:89fb:4c73:bbd:c30b:2665]:80 -go.forward.peer  tcp://yggdrasil.sunsung.fun:4442
```

## Probe

```bash
./ratatoskr -go.probe.trace a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924 -go.probe.peer tcp://yggdrasil.sunsung.fun:4442
```

```bash
./ratatoskr -go.probe.scan -go.probe.peer tcp://yggdrasil.sunsung.fun:4442
```

```bash
./ratatoskr -go.probe.ping a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924 -go.probe.peer tcp://yggdrasil.sunsung.fun:4442
```
