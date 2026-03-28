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
./ratatoskr -go.traceroute.trace a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924 -go.traceroute.peer tcp://yggdrasil.sunsung.fun:4442
```

```bash
./ratatoskr -go.traceroute.scan -go.traceroute.peer tcp://yggdrasil.sunsung.fun:4442
```