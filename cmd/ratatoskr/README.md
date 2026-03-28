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

## Key generation

```bash
./ratatoskr -go.keygen 1s
```

```bash
./ratatoskr -go.keygen 10s
```

```bash
./ratatoskr -go.keygen 1m
```

## Traceroute

```bash
./ratatoskr -go.trace.trace a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924 -go.trace.peer tls://ygg5.mk16.de:1338?key=0000009611ae5391dc0aceea9f3fa6a0dc1279f4306059339e84bfb8b74d2f9b
```

```bash
./ratatoskr -go.trace.scan -go.trace.peer tcp://1.2.3.4:5678
```